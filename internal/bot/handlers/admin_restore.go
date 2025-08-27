package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers/migrations"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pressly/goose/v3"
)

// простейший FSM по chatID
var restoreWaiting = map[int64]bool{}

func AdminRestoreFSMActive(chatID int64) bool { return restoreWaiting[chatID] }

func HandleAdminRestoreStart(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	user, _ := db.GetUserByTelegramID(database, chatID)
	if user == nil || user.Role == nil || *user.Role != "admin" {
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Только для администратора"))
		return
	}
	restoreWaiting[chatID] = true
	text := "⚠️ Восстановление перезапишет данные в существующих таблицах.\n\n" +
		"Пришлите ZIP, полученный кнопкой «💾 Бэкап БД». Я загружу файл и восстановлю данные."
	bot.Send(tgbotapi.NewMessage(chatID, text))
}

func HandleAdminRestoreMessage(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	if !AdminRestoreFSMActive(chatID) {
		return
	}
	if msg.Document == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Пришлите ZIP-файл с бэкапом."))
		return
	}
	defer func() { delete(restoreWaiting, chatID) }()

	// качаем файл из Telegram
	path, err := downloadTelegramFile(bot, msg.Document.FileID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось скачать файл: %v", err)))
		return
	}
	defer os.Remove(path)

	bot.Send(tgbotapi.NewMessage(chatID, "⌛ Восстанавливаю БД из бэкапа…"))
	if err := restoreFromZip(database, path); err != nil {
		log.Println("restore error:", err)
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Ошибка восстановления: %v", err)))
		return
	}
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Готово. База восстановлена."))
}

func downloadTelegramFile(bot *tgbotapi.BotAPI, fileID string) (string, error) {
	f, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return "", err
	}
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return "", errors.New("BOT_TOKEN не задан")
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, f.FilePath)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("telegram file status: %s", resp.Status)
	}
	tmp := filepath.Join(os.TempDir(), "restore_"+time.Now().Format("20060102_150405")+".zip")
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}
	return tmp, nil
}

func restoreFromZip(database *sql.DB, zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	// Если таблиц нет — создадим схему миграциями
	if err := ensureSchema(database); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	// читаем все CSV в память (обычно объёмы умеренные)
	type tableDump struct {
		name string
		csv  *csv.Reader
		r    io.ReadCloser
	}
	var dumps []tableDump
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "data/") && strings.HasSuffix(f.Name, ".csv") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			// оборачиваем, чтобы csv.Reader не закрыл нам zip-ридер раньше времени
			buf := &bytes.Buffer{}
			if _, err := io.Copy(buf, rc); err != nil {
				rc.Close()
				return err
			}
			rc.Close()
			r := io.NopCloser(bytes.NewReader(buf.Bytes()))
			dumps = append(dumps, tableDump{
				name: strings.TrimSuffix(filepath.Base(f.Name), ".csv"),
				csv:  csv.NewReader(bytes.NewReader(buf.Bytes())),
				r:    r,
			})
		}
	}
	if len(dumps) == 0 {
		return errors.New("в архиве нет data/*.csv")
	}

	// порядок загрузки (учёт FK)
	order := []string{
		"users", "classes", "categories", "periods", "parents_students",
		"scores", "role_changes", "score_levels",
	}
	index := map[string]tableDump{}
	for _, d := range dumps {
		index[d.name] = d
	}

	tx, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// TRUNCATE (если таблицы существуют) и заливка
	for _, t := range order {
		d, ok := index[t]
		if !ok {
			continue
		}
		exists, err := tableExists(tx, t)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.Exec(`TRUNCATE ` + quoteIdent(t) + ` CASCADE`); err != nil {
				return fmt.Errorf("truncate %s: %w", t, err)
			}
		} else {
			log.Println("info: skip truncate — table not exists:", t)
		}
		if err := loadCSV(tx, t, d.csv); err != nil {
			return fmt.Errorf("load %s: %w", t, err)
		}
		if err := resetSequence(tx, t); err != nil {
			return fmt.Errorf("reset seq %s: %w", t, err)
		}
	}
	// загружаем «остальные» таблицы из архива (если были новые)
	for _, d := range dumps {
		skip := false
		for _, t := range order {
			if t == d.name {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		exists, err := tableExists(tx, d.name)
		if err != nil {
			return err
		}
		if !exists {
			log.Println("info: skip restore — table not exists in DB:", d.name)
			continue
		}
		if _, err := tx.Exec(`TRUNCATE ` + quoteIdent(d.name) + ` CASCADE`); err != nil {
			return fmt.Errorf("truncate %s: %w", d.name, err)
		}
		if err := loadCSV(tx, d.name, d.csv); err != nil {
			return fmt.Errorf("load %s: %w", d.name, err)
		}
		if err := resetSequence(tx, d.name); err != nil {
			return fmt.Errorf("reset seq %s: %w", d.name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
func ensureSchema(database *sql.DB) error {
	// проверим наличие таблиц
	var n int
	if err := database.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// накатываем миграции из embed FS
	goose.SetBaseFS(migrations.FS)
	return goose.Up(database, ".")
}

func loadCSV(tx *sql.Tx, table string, r *csv.Reader) error {
	cols, err := r.Read()
	if err != nil {
		return err
	}
	// узнаём типы колонок из information_schema (нужно для корректного парсинга дат/времен)
	colTypes, err := getColumnTypes(tx, table)
	if err != nil {
		return err
	}

	ph := make([]string, len(cols))
	for i := range cols {
		ph[i] = "$" + strconv.Itoa(i+1)
	}
	// безопасная сборка SQL без fmt.Sprintf + квотирование идентификаторов
	qcols := make([]string, len(cols))
	for i, c := range cols {
		qcols[i] = quoteIdent(c)
	}
	stmt := "INSERT INTO " + quoteIdent(table) +
		" (" + strings.Join(qcols, ",") + ") VALUES (" + strings.Join(ph, ",") + ")"

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		args := make([]any, len(rec))
		for i, v := range rec {
			s := strings.TrimSpace(v)
			// принимаем старые бэкапы: "", "<nil>", "null" → NULL
			if s == "" || s == "<nil>" || strings.EqualFold(s, "null") {
				args[i] = nil
				continue
			}
			// преобразуем по типу колонки
			t := strings.ToLower(colTypes[cols[i]])
			switch {
			case strings.Contains(t, "timestamp") || t == "date":
				if tt, perr := parseTimeFlex(s); perr == nil {
					args[i] = tt
				} else {
					// в крайнем случае отправим как строку (пусть упадёт на конкретном месте)
					args[i] = s
				}
			case strings.Contains(t, "boolean"):
				// пусть PG сам приведёт из 't/true/1' — строку отправляем как есть
				args[i] = s
			case strings.Contains(t, "integer") || strings.Contains(t, "bigint") ||
				strings.Contains(t, "smallint") || strings.Contains(t, "numeric") ||
				strings.Contains(t, "double"):
				// числа удобно оставить строкой — PG приведёт, но без "<nil>"
				args[i] = s
			default:
				args[i] = s
			}
		}
		if _, err := tx.Exec(stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// Получаем map[column_name]data_type для таблицы
func getColumnTypes(tx *sql.Tx, table string) (map[string]string, error) {
	rows, err := tx.Query(`
        SELECT column_name, data_type
        FROM information_schema.columns
        WHERE table_schema='public' AND table_name = $1
    `, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var name, dt string
		if err := rows.Scan(&name, &dt); err != nil {
			return nil, err
		}
		m[name] = dt
	}
	return m, rows.Err()
}

// Парсим время из нескольких распространённых форматов (RFC3339/RFC3339Nano и Go-строка с зоной типа "MSK")
func parseTimeFlex(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST", // например: 2025-08-24 04:13:10.69002 +0300 MSK
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02",
	}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

func resetSequence(tx *sql.Tx, table string) error {
	// 1) Есть ли вообще колонка id?
	var hasID bool
	if err := tx.QueryRow(`
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.columns
		  WHERE table_schema='public' AND table_name=$1 AND column_name='id'
		)`, table).Scan(&hasID); err != nil {
		return err
	}
	if !hasID {
		return nil // у таблицы нет id — нечего сбрасывать
	}

	// 2) Есть ли последовательность у id?
	var seq sql.NullString
	if err := tx.QueryRow(`SELECT pg_get_serial_sequence($1,'id')`, table).Scan(&seq); err != nil {
		return err
	}
	if !seq.Valid || seq.String == "" {
		return nil // id не serial/identity — сброс не нужен
	}

	// 3) Вычисляем MAX(id); если строк нет — ставим value=1, is_called=false
	qTable := quoteIdent(table)
	var maxID sql.NullInt64
	if err := tx.QueryRow("SELECT MAX(id) FROM " + qTable).Scan(&maxID); err != nil {
		return err
	}
	var value int64 = 1
	var isCalled bool = false
	if maxID.Valid {
		if maxID.Int64 >= 1 {
			value = maxID.Int64
			isCalled = true
		} else {
			value = 1
			isCalled = false
		}
	}
	// 4) setval по найденной последовательности
	_, err := tx.Exec(`SELECT setval($1::regclass, $2, $3)`, seq.String, value, isCalled)
	return err
}

// tableExists — проверка существования таблицы в public
func tableExists(tx *sql.Tx, table string) (bool, error) {
	var exists bool
	err := tx.QueryRow(`
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.tables
		  WHERE table_schema='public' AND table_name=$1
		)`, table).Scan(&exists)
	return exists, err
}
