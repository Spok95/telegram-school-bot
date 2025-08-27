package handlers

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers/migrations"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// HandleAdminBackup — создаёт ZIP с CSV-дампами всех таблиц + миграциями
func HandleAdminBackup(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	user, err := db.GetUserByTelegramID(database, chatID)
	if err != nil || user == nil || user.Role == nil || *user.Role != "admin" {
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Только для администратора"))
		return
	}
	bot.Send(tgbotapi.NewMessage(chatID, "⌛ Делаю бэкап базы…"))

	path, size, err := dumpDatabaseToZip(database)
	if err != nil {
		log.Println("backup error:", err)
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось сделать бэкап: %v", err)))
		return
	}
	defer os.Remove(path)

	// ~50 МБ лимит Telegram-документов. Берём запас.
	if size > 48*1024*1024 {
		bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("⚠️ Бэкап получился %d МБ — слишком большой для Telegram. "+
				"Сделайте full dump: docker compose exec -T postgres pg_dump -U school -d school | gzip > backup.sql.gz", size/1024/1024)))
		return
	}

	f, _ := os.Open(path)
	defer f.Close()
	// В текущей версии go-telegram-bot-api у FileReader нет поля Size
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Reader: f,
		Name:   filepath.Base(path),
	})
	doc.Caption = "💾 Резервная копия базы (CSV + миграции)"
	bot.Send(doc)
}

// dumpDatabaseToZip: выгрузка всех таблиц public-схемы в CSV + вложение миграций и metadata.json
func dumpDatabaseToZip(database *sql.DB) (string, int64, error) {
	rows, err := database.Query(`
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema='public' AND table_type='BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return "", 0, err
		}
		tables = append(tables, t)
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	// metadata.json
	meta := fmt.Sprintf(`{"created_at":"%s","version":"%s"}`,
		time.Now().Format(time.RFC3339), os.Getenv("GIT_SHA"))
	if w, err := zw.Create("metadata.json"); err == nil {
		_, _ = w.Write([]byte(meta))
	}

	// миграции (из embed FS)
	_ = fsWalk(migrations.FS, ".", func(path string, data []byte) error {
		w, err := zw.Create(filepath.Join("migrations", path))
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})

	// CSV по таблицам
	for _, table := range tables {
		if strings.HasPrefix(table, "pg_") || table == "goose_db_version" {
			continue
		}
		if err := writeTableCSV(database, zw, table); err != nil {
			return "", 0, fmt.Errorf("table %s: %w", table, err)
		}
	}

	if err := zw.Close(); err != nil {
		return "", 0, err
	}

	filename := fmt.Sprintf("backup_%s.zip", time.Now().Format("20060102_150405"))
	path := filepath.Join(os.TempDir(), filename)
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return "", 0, err
	}
	return path, int64(buf.Len()), nil
}

func writeTableCSV(database *sql.DB, zw *zip.Writer, table string) error {
	w, err := zw.Create(fmt.Sprintf("data/%s.csv", table))
	if err != nil {
		return err
	}
	query := fmt.Sprintf("SELECT * FROM"+" %s", quoteIdent(table))
	rows, err := database.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(cols); err != nil {
		return err
	}

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		rec := make([]string, len(cols))
		for i, v := range vals {
			// NULL → пустая строка (совместимо с импортом)
			if v == nil {
				rec[i] = ""
				continue
			}
			// []byte → текст
			if b, ok := v.([]byte); ok {
				rec[i] = string(b)
				continue
			}
			// time.Time → стабильный формат
			if t, ok := v.(time.Time); ok {
				rec[i] = t.Format(time.RFC3339Nano)
				continue
			}
			// остальное как строка
			rec[i] = fmt.Sprint(v)
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// quoteIdent — экранирует идентификатор для SQL
func quoteIdent(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

// fsWalk обход embed.FS
func fsWalk(fsys fs.ReadFileFS, root string, fn func(path string, data []byte) error) error {
	rd, ok := fsys.(fs.ReadDirFS)
	if !ok {
		return nil
	}
	entries, err := rd.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			if err := fsWalk(fsys, p, fn); err != nil {
				return err
			}
			continue
		}
		data, err := fsys.ReadFile(p)
		if err != nil {
			return err
		}
		if err := fn(strings.TrimPrefix(p, "./"), data); err != nil {
			return err
		}
	}
	return nil
}
