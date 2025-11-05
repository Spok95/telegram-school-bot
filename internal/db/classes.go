package db

import (
	"context"
	"database/sql"
)

type Class struct {
	ID     int64
	Number int
	Letter string
	Hidden bool
}

func ListClassNumbers(ctx context.Context, database *sql.DB) ([]int, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT number
		FROM classes
		WHERE hidden = FALSE
		ORDER BY number
	`)
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
		SELECT id, number, letter
		FROM classes
		WHERE number = $1
		  AND hidden = FALSE
		ORDER BY letter
	`, number)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Class
	for rows.Next() {
		var c Class
		if err := rows.Scan(&c.ID, &c.Number, &c.Letter); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func GetClassByID(ctx context.Context, database *sql.DB, id int64) (*Class, error) {
	row := database.QueryRowContext(ctx, `
        SELECT id, number, letter, hidden
        FROM classes
        WHERE id = $1
    `, id)

	var c Class
	if err := row.Scan(&c.ID, &c.Number, &c.Letter, &c.Hidden); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// GetClassByNumberLetter — вернуть класс по номеру и букве (без учёта регистра буквы).
func GetClassByNumberLetter(ctx context.Context, database *sql.DB, number int, letter string) (*Class, error) {
	row := database.QueryRowContext(ctx, `
		SELECT id, number, letter
		FROM classes
		WHERE number = $1 AND lower(letter) = lower($2)
		LIMIT 1
	`, number, letter)
	var c Class
	if err := row.Scan(&c.ID, &c.Number, &c.Letter); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func ListVisibleClasses(ctx context.Context, dbx *sql.DB) ([]Class, error) {
	rows, err := dbx.QueryContext(ctx, `
        SELECT id, number, letter, hidden
        FROM classes
        WHERE hidden = FALSE
        ORDER BY number, letter
    `)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var res []Class
	for rows.Next() {
		var c Class
		if err := rows.Scan(&c.ID, &c.Number, &c.Letter, &c.Hidden); err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, rows.Err()
}

func SetClassHidden(ctx context.Context, database *sql.DB, id int64, hidden bool) error {
	_, err := database.ExecContext(ctx, `
        UPDATE classes
        SET hidden = $2
        WHERE id = $1
    `, id, hidden)
	return err
}

func ListAllClasses(ctx context.Context, database *sql.DB) ([]Class, error) {
	rows, err := database.QueryContext(ctx, `
        SELECT id, number, letter, hidden
        FROM classes
        ORDER BY number, letter
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Class
	for rows.Next() {
		var c Class
		if err := rows.Scan(&c.ID, &c.Number, &c.Letter, &c.Hidden); err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, rows.Err()
}
