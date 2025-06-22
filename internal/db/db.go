package db

import (
	"database/sql"
	"log"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB() {
	var err error
	DB, err = sql.Open("sqlite", "school.db")
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatal("Ошибка подключения к базе данных:", err)
	}

	log.Println("База данных успешно подключена")
}
