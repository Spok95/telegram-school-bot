package db

import (
	"database/sql"
	"fmt"
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

	p.StartDate, _ = time.Parse("02.01.2006", startStr)
	p.EndDate, _ = time.Parse("02.01.2006", endStr)
	return &p, nil
}

func SetActivePeriod(database *sql.DB) error {
	now := time.Now()

	_, err := database.Exec(`UPDATE periods SET is_active = 0`)
	if err != nil {
		return err
	}

	_, err = database.Exec(`UPDATE periods SET is_active = 1 WHERE start_date <= ? AND end_date >= ?`, now, now)
	if err != nil {
		return err
	}

	return nil
}

func CreatePeriod(database *sql.DB, p models.Period) (int64, error) {
	if p.StartDate.After(p.EndDate) {
		return 0, fmt.Errorf("дата окончания не может быть раньше даты начала")
	}

	res, err := database.Exec(`
		INSERT INTO periods (name, start_date, end_date, is_active) 
		VALUES (?, ?, ?, ?)`,
		p.Name,
		p.StartDate.Format("02.01.2006"),
		p.EndDate.Format("02.01.2006"),
		p.IsActive,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
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
		p.StartDate, _ = time.Parse("02.01.2006", startStr)
		p.EndDate, _ = time.Parse("02.01.2006", endStr)
		result = append(result, p)
	}
	return result, nil
}

func GetPeriodByID(database *sql.DB, id int) (*models.Period, error) {
	row := database.QueryRow(`SELECT id, name, start_date, end_date, is_active FROM periods WHERE id = ?`, id)

	var p models.Period
	var startStr, endStr string
	err := row.Scan(&p.ID, &p.Name, &startStr, &endStr, &p.IsActive)
	if err != nil {
		return nil, err
	}
	p.StartDate, _ = time.Parse("02.01.2006", startStr)
	p.EndDate, _ = time.Parse("02.01.2006", endStr)
	return &p, nil
}
