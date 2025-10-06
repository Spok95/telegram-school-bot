package db

import (
	"database/sql"
	"fmt"

	"github.com/Spok95/telegram-school-bot/internal/models"
)

func GetActivePeriod(database *sql.DB) (*models.Period, error) {
	row := database.QueryRow(`
		SELECT id, name, start_date, end_date, is_active
		FROM periods
		WHERE is_active = TRUE 
		LIMIT 1`)

	var p models.Period
	if err := row.Scan(&p.ID, &p.Name, &p.StartDate, &p.EndDate, &p.IsActive); err != nil {
		return nil, err
	}
	return &p, nil
}

func SetActivePeriod(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`UPDATE periods SET is_active = FALSE`); err != nil {
		return err
	}

	if _, err = tx.Exec(`
		UPDATE periods SET is_active = TRUE
		WHERE start_date <= CURRENT_DATE AND end_date >= CURRENT_DATE`); err != nil {
		return err
	}

	return tx.Commit()
}

func CreatePeriod(database *sql.DB, p models.Period) (int64, error) {
	if p.StartDate.After(p.EndDate) {
		return 0, fmt.Errorf("дата окончания не может быть раньше даты начала")
	}
	var id int64
	err := database.QueryRow(`
		INSERT INTO periods (name, start_date, end_date, is_active)
		VALUES ($1, $2, $3, FALSE)
		RETURNING id
	`, p.Name, p.StartDate, p.EndDate).Scan(&id)
	return id, err
}

func UpdatePeriod(database *sql.DB, p models.Period) error {
	if p.StartDate.After(p.EndDate) {
		return fmt.Errorf("дата окончания не может быть раньше даты начала")
	}
	_, err := database.Exec(`
		UPDATE periods SET name = $1, start_date = $2, end_date = $3
		WHERE id = $4
	`, p.Name, p.StartDate, p.EndDate, p.ID)
	return err
}

func ListPeriods(database *sql.DB) ([]models.Period, error) {
	rows, err := database.Query(`
		SELECT id, name, start_date, end_date, is_active
		FROM periods
		ORDER BY start_date`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.Period
	for rows.Next() {
		var p models.Period
		if err := rows.Scan(&p.ID, &p.Name, &p.StartDate, &p.EndDate, &p.IsActive); err != nil {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

func GetPeriodByID(database *sql.DB, id int) (*models.Period, error) {
	row := database.QueryRow(`
		SELECT id, name, start_date, end_date, is_active
		FROM periods
		WHERE id = $1`, id)
	var p models.Period
	if err := row.Scan(&p.ID, &p.Name, &p.StartDate, &p.EndDate, &p.IsActive); err != nil {
		return nil, err
	}
	return &p, nil
}
