package db

import (
	"database/sql"
	"log"
	_ "modernc.org/sqlite"
	"path/filepath"
)

var DB *sql.DB

func InitDB() {
	dbPath := "./data/school.db"
	absPath, _ := filepath.Abs(dbPath)
	log.Println("Открывается база данных по пути:", absPath)

	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatal("Ошибка подключения к базе данных:", err)
	}

	log.Println("База данных успешно подключена")
}
