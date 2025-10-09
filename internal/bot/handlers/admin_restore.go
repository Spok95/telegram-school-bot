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

	"github.com/Spok95/telegram-school-bot/internal/backupclient"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers/migrations"
	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/observability"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pressly/goose/v3"
)

// HandleAdminRestoreLatest — восстанавливает БД из последнего файла в ./backups через sidecar
func HandleAdminRestoreLatest(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	ctx, cancel := ctxutil.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	user, _ := db.GetUserByTelegramID(ctx, database, chatID)
	if user == nil || user.Role == nil || *user.Role != "admin" {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Только для администратора")); err != nil {
			metrics.HandlerErrors.Inc()
			observability.CaptureErr(err)
		}
		return
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🛠 Восстанавливаю БД из последнего бэкапа…")); err != nil {
		metrics.HandlerErrors.Inc()
		observability.CaptureErr(err)
	}

	path, err := backupclient.RestoreLatest(ctx)
	if err != nil {
		metrics.HandlerErrors.Inc()
		observability.CaptureErr(err)
		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось восстановить: %v", err)))
		return
	}

	_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Готово. Восстановлено из: "+path))
}

// простейший FSM по chatID
var restoreWaiting = map[int64]bool{}

func AdminRestoreFSMActive(chatID int64) bool { return restoreWaiting[chatID] }

func HandleAdminRestoreStart(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	user, _ := db.GetUserByTelegramID(ctx, database, chatID)
	if user == nil || user.Role == nil || *user.Role != "admin" {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Только для администратора")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	restoreWaiting[chatID] = true

	text := "⚠️ Восстановление перезапишет данные в существующих таблицах.\n\n" +
		"Пришлите ZIP, полученный кнопкой «💾 Бэкап БД». Я загружу файл и восстановлю данные."

	cancel := tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "restore_cancel")
	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(cancel),
	)

	if _, err := tg.Send(bot, m); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func HandleAdminRestoreCallback(ctx context.Context, bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	chatID := cb.Message.Chat.ID
	if cb.Data == "restore_cancel" {
		delete(restoreWaiting, chatID)

		// Снимем клавиатуру у сообщения с приглашением, чтобы не висела
		_, _ = tg.Send(bot, tgbotapi.NewEditMessageReplyMarkup(
			chatID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}),
		)

		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Восстановление из файла отменено."))
	}
}

func HandleAdminRestoreMessage(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	ctx, cancel := ctxutil.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	chatID := msg.Chat.ID
	if !AdminRestoreFSMActive(chatID) {
		return
	}
	if msg.Document == nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Пришлите ZIP-файл с бэкапом.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { delete(restoreWaiting, chatID) }()

	// качаем файл из Telegram
	path, err := downloadTelegramFile(ctx, bot, msg.Document.FileID)
	if err != nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось скачать файл: %v", err))); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { _ = os.Remove(path) }()

	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⌛ Восстанавливаю БД из бэкапа…")); err != nil {
		metrics.HandlerErrors.Inc()
	}

	if err := restoreFromZip(ctx, database, path); err != nil {
		log.Println("restore error:", err)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Ошибка восстановления: %v", err))); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Готово. База восстановлена.")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func downloadTelegramFile(ctx context.Context, bot *tgbotapi.BotAPI, fileID string) (string, error) {
	f, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return "", err
	}
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return "", errors.New("BOT_TOKEN не задан")
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("telegram file status: %s", resp.Status)
	}
	tmp := filepath.Join(os.TempDir(), "restore_"+time.Now().Format("20060102_150405")+".zip")
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}
	return tmp, nil
}

func restoreFromZip(ctx context.Context, database *sql.DB, zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	// Если таблиц нет — создадим схему миграциями
	if err := ensureSchemaContext(ctx, database); err != nil {
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
				_ = rc.Close()
				return err
			}
			_ = rc.Close()
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

	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// TRUNCATE (если таблицы существуют) и заливка
	for _, t := range order {
		d, ok := index[t]
		if !ok {
			continue
		}
		exists, err := tableExistsContext(ctx, tx, t)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, `TRUNCATE `+quoteIdent(t)+` CASCADE`); err != nil {
				return fmt.Errorf("truncate %s: %w", t, err)
			}
		} else {
			log.Println("info: skip truncate — table not exists:", t)
		}
		if err := loadCSVContext(ctx, tx, t, d.csv); err != nil {
			return fmt.Errorf("load %s: %w", t, err)
		}
		if err := resetSequenceContext(ctx, tx, t); err != nil {
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
		exists, err := tableExistsContext(ctx, tx, d.name)
		if err != nil {
			return err
		}
		if !exists {
			log.Println("info: skip restore — table not exists in DB:", d.name)
			continue
		}
		if _, err := tx.ExecContext(ctx, `TRUNCATE `+quoteIdent(d.name)+` CASCADE`); err != nil {
			return fmt.Errorf("truncate %s: %w", d.name, err)
		}
		if err := loadCSVContext(ctx, tx, d.name, d.csv); err != nil {
			return fmt.Errorf("load %s: %w", d.name, err)
		}
		if err := resetSequenceContext(ctx, tx, d.name); err != nil {
			return fmt.Errorf("reset seq %s: %w", d.name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func ensureSchemaContext(ctx context.Context, database *sql.DB) error {
	// проверим наличие таблиц
	var n int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// накатываем миграции из embed FS
	goose.SetBaseFS(migrations.FS)
	return goose.Up(database, ".")
}

func loadCSVContext(ctx context.Context, tx *sql.Tx, table string, r *csv.Reader) error {
	cols, err := r.Read()
	if err != nil {
		return err
	}
	// узнаём типы колонок из information_schema (нужно для корректного парсинга дат/времен)
	colTypes, err := getColumnTypesContext(ctx, tx, table)
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
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// Получаем map[column_name]data_type для таблицы
func getColumnTypesContext(ctx context.Context, tx *sql.Tx, table string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `
        SELECT column_name, data_type
        FROM information_schema.columns
        WHERE table_schema='public' AND table_name = $1
    `, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func resetSequenceContext(ctx context.Context, tx *sql.Tx, table string) error {
	// 1) Есть ли вообще колонка id?
	var hasID bool
	if err := tx.QueryRowContext(ctx, `
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
	if err := tx.QueryRowContext(ctx, `SELECT pg_get_serial_sequence($1,'id')`, table).Scan(&seq); err != nil {
		return err
	}
	if !seq.Valid || seq.String == "" {
		return nil // id не serial/identity — сброс не нужен
	}

	// 3) Вычисляем MAX(id); если строк нет — ставим value=1, is_called=false
	qTable := quoteIdent(table)
	var maxID sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(id) FROM "+qTable).Scan(&maxID); err != nil {
		return err
	}
	var value int64 = 1
	isCalled := false
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
	_, err := tx.ExecContext(ctx, `SELECT setval($1::regclass, $2, $3)`, seq.String, value, isCalled)
	return err
}

// tableExists — проверка существования таблицы в public
func tableExistsContext(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.tables
		  WHERE table_schema='public' AND table_name=$1
		)`, table).Scan(&exists)
	return exists, err
}

// quoteIdent — экранирует идентификатор для SQL
func quoteIdent(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}
