package db

func Migrate() {
	createUsers := `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id INTEGER NOT NULL UNIQUE,
    name TEXT,
    role TEXT,
    class_id INTEGER,
    child_id INTEGER
);`
	_, err := DB.Exec(createUsers)
	if err != nil {
		panic("Ошибка при создании таблицы users: " + err.Error())
	}

	createScoreLog := `
CREATE TABLE IF NOT EXISTS score_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id INTEGER NOT NULL,
    category TEXT NOT NULL,
    points INTEGER NOT NULL,
    type TEXT NOT NULL,
    comment TEXT,
    approved BOOLEAN DEFAULT false,
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	_, err = DB.Exec(createScoreLog)
	if err != nil {
		panic("Ошибка при создании таблицы score_log: " + err.Error())
	}
}
