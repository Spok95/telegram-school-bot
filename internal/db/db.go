package db

import (
	"database/sql"
	"log"
	_ "modernc.org/sqlite"
	"path/filepath"
)

func Init() (*sql.DB, error) {
	dbPath := "./data/school.db"
	absPath, _ := filepath.Abs(dbPath)
	log.Println("Открывается база данных по пути:", absPath)

	var err error
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	return db, nil
}
