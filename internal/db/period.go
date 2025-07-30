package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"time"
)

func GetActivePeriod(database *sql.DB) (*models.Period, error) {
	row := database.QueryRow(`SELECT id, name, start_date, end_date, is_active FROM periods WHERE is_active = 1 LIMIT 1`)

	var p models.Period
	var startStr, endStr string
	err := row.Scan(&p.ID, &p.Name, &startStr, &endStr, &p.IsActive)
	if err != nil {
		return nil, err
	}

	p.StartDate, _ = time.Parse("2006-01-02", startStr)
	p.EndDate, _ = time.Parse("2006-01-02", endStr)
	return &p, nil
}

func SetActivePeriod(database *sql.DB, periodID int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Снимаем флаг is_active у всех периодов
	_, err = tx.Exec(`UPDATE periods SET is_active = 0`)
	if err != nil {
		return err
	}

	// Устанавливаем is_active = 1 выбранному периоду
	_, err = tx.Exec(`UPDATE periods SET is_active = 1 WHERE id = ?`, periodID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func CreatePeriod(database *sql.DB, p models.Period) error {
	_, err := database.Exec(`
		INSERT INTO periods (name, start_date, end_date, is_active) 
		VALUES (?, ?, ?, ?)`,
		p.Name,
		p.StartDate.Format("2006-01-02"),
		p.EndDate.Format("2006-01-02"),
		p.IsActive,
	)
	return err
}

func GetLastInsertID(database *sql.DB) int64 {
	var id int64
	_ = database.QueryRow("SELECT last_insert_rowid();").Scan(&id)
	return id
}

func ListPeriods(database *sql.DB) ([]models.Period, error) {
	rows, err := database.Query(`SELECT id, name, start_date, end_date, is_active FROM periods ORDER BY start_date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Period
	for rows.Next() {
		var p models.Period
		var startStr, endStr string
		if err := rows.Scan(&p.ID, &p.Name, &startStr, &endStr, &p.IsActive); err != nil {
			continue
		}
		p.StartDate, _ = time.Parse("2006-01-02", startStr)
		p.EndDate, _ = time.Parse("2006-01-02", endStr)
		result = append(result, p)
	}
	return result, nil
}
