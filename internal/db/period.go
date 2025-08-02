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
	res, err := database.Exec(`
		INSERT INTO periods (name, start_date, end_date, is_active) 
		VALUES (?, ?, ?, ?)`,
		p.Name,
		p.StartDate.Format("2006-01-02"),
		p.EndDate.Format("2006-01-02"),
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
		p.StartDate, _ = time.Parse("2006-01-02", startStr)
		p.EndDate, _ = time.Parse("2006-01-02", endStr)
		result = append(result, p)
	}
	return result, nil
}

func GetScoresByPeriod(database *sql.DB, periodID int) ([]models.ScoreWithUser, error) {
	query := `
	SELECT
		s.name AS student_name,
		s.class_number,
		s.class_letter,
		c.label AS category_label,
		scores.points,
		scores.comment,
		a.name AS added_by_name,
		scores.created_at
	FROM scores
	JOIN users s ON scores.student_id = s.id
	JOIN users a ON scores.created_by = a.id
	JOIN categories c ON scores.category_id = c.id
	WHERE scores.period_id = ?
	ORDER BY scores.created_at ASC;
	`
	rows, err := database.Query(query, periodID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []models.ScoreWithUser
	for rows.Next() {
		var s models.ScoreWithUser
		if err := rows.Scan(&s.StudentName, &s.ClassNumber, &s.ClassLetter, &s.CategoryLabel, &s.Points, &s.Comment, &s.AddedByName, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
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
	p.StartDate, _ = time.Parse("2006-01-02", startStr)
	p.EndDate, _ = time.Parse("2006-01-02", endStr)
	return &p, nil
}
