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
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    class_id INTEGER,
    class_name TEXT,
    class_number INTEGER,
    class_letter TEXT,
    child_id INTEGER,
    confirmed BOOLEAN DEFAULT 0,
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
    type TEXT NOT NULL CHECK(type IN ('add', 'remove')),
    comment TEXT,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
    approved_by INTEGER,
    approved_at TIMESTAMP,
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    period_id INTEGER,
    FOREIGN KEY (student_id) REFERENCES users(id),
    FOREIGN KEY (category_id) REFERENCES categories(id),
    FOREIGN KEY (approved_by) REFERENCES users(id),
    FOREIGN KEY (created_by) REFERENCES users(id),
    FOREIGN KEY (period_id) REFERENCES periods(id)
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
    UNIQUE(value, label, category_id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
`)
	if err != nil {
		return logError("score_levels", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS classes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    number INTEGER NOT NULL,
    letter TEXT NOT NULL,
    collective_score INTEGER DEFAULT 0,
    UNIQUE(number, letter)
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
    end_date TEXT NOT NULL,
    is_active INTEGER DEFAULT 0
);

`)
	if err != nil {
		return logError("periods", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS role_changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    old_role TEXT,
    new_role TEXT,
    changed_by INTEGER,
    changed_at TEXT
);
`)
	if err != nil {
		return logError("role_changes", err)
	}
	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS parent_link_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id INTEGER NOT NULL,
  student_id INTEGER NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		return logError("parent_link_requests", err)
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
