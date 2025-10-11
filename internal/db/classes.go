package db

import (
	"context"
	"database/sql"
)

type Class struct {
	ID     int64
	Number int
	Letter string
	Name   string
}

func ListClassNumbers(ctx context.Context, database *sql.DB) ([]int, error) {
	rows, err := database.QueryContext(ctx, `SELECT DISTINCT number FROM classes ORDER BY number`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []int{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func ListClassesByNumber(ctx context.Context, database *sql.DB, number int) ([]Class, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id, number, letter, name
		FROM classes
		WHERE number = $1
		ORDER BY letter
	`, number)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Class
	for rows.Next() {
		var c Class
		if err := rows.Scan(&c.ID, &c.Number, &c.Letter, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func GetClassByID(ctx context.Context, database *sql.DB, id int64) (*Class, error) {
	row := database.QueryRowContext(ctx, `SELECT id, number, letter, name FROM classes WHERE id = $1`, id)
	var c Class
	if err := row.Scan(&c.ID, &c.Number, &c.Letter, &c.Name); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}
