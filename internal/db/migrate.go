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
}
