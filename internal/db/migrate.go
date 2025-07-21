package db

import (
	"database/sql"
	"log"
)

func Migrate(database *sql.DB) error {
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
	if _, err := database.Exec(createUsers); err != nil {
		return logError("users", err)
	}

	createScores := `
CREATE TABLE IF NOT EXISTS scores (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id INTEGER NOT NULL,
    category_id INTEGER NOT NULL,
    points INTEGER NOT NULL,
    type TEXT NOT NULL, -- 'add' или 'remove'
    comment TEXT,
    approved BOOLEAN DEFAULT false,
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);`

	if _, err := database.Exec(createScores); err != nil {
		return logError("scores", err)
	}

	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS parents_students (
    parent_id INTEGER REFERENCES users(id),
    student_id INTEGER REFERENCES users(id),
    PRIMARY KEY (parent_id, student_id)
);
`)
	if err != nil {
		return logError("parents_students", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    label TEXT NOT NULL
);
`)
	if err != nil {
		return logError("categories", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS score_levels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    value INTEGER NOT NULL,
    label TEXT NOT NULL,
    category_id INTEGER,
    UNIQUE(value, label, category_id)
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
`)
	if err != nil {
		return logError("score_levels", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS classes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    collective_score INTEGER DEFAULT 0
);
`)
	if err != nil {
		return logError("classes", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS periods (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL
);
`)
	if err != nil {
		return logError("periods", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS role_changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id INTEGER NOT NULL,
    old_role TEXT,
    new_role TEXT NOT NULL,
    changed_by INTEGER NOT NULL,
    changed_at TEXT NOT NULL
);
`)
	if err != nil {
		return logError("role_changes", err)
	}
	if err := SeedScoreLevels(database); err != nil {
		log.Fatal("❌ Ошибка при наполнении таблиц:", err)
	}

	log.Println("✅ Миграция выполнена успешно.")
	return nil
}

func logError(table string, err error) error {
	log.Printf("❌ Ошибка при создании таблицы %s: %v\n", table, err)
	return err
}
