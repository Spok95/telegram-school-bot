package db

import (
	"database/sql"
	"log"
)

func Migrate(db *sql.DB) error {
	createUsers := `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id INTEGER NOT NULL UNIQUE,
    name TEXT,
    role TEXT,
    class_id INTEGER,
    class_name TEXT,
    child_id INTEGER,
    pending_role TEXT,
    pending_fio TEXT,
    pending_class TEXT,
    pending_childfio TEXT,
    is_active BOOLEAN DEFAULT 1
);`
	if _, err := db.Exec(createUsers); err != nil {
		return logError("users", err)
	}

	createScores := `
CREATE TABLE IF NOT EXISTS scores (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id INTEGER NOT NULL,
    category TEXT NOT NULL,
    points INTEGER NOT NULL,
    type TEXT NOT NULL, -- 'add' или 'remove'
    comment TEXT,
    approved BOOLEAN DEFAULT false,
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := db.Exec(createScores); err != nil {
		return logError("scores", err)
	}

	log.Println("✅ Миграция выполнена успешно.")
	return nil
}

func logError(table string, err error) error {
	log.Printf("❌ Ошибка при создании таблицы %s: %v\n", table, err)
	return err
}
