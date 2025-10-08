package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
)

type UserRow struct {
	Name     string
	Role     string
	ClassNum sql.NullInt64
	ClassLet sql.NullString
}

type StudentRow struct {
	Name       string
	ClassNum   sql.NullInt64
	ClassLet   sql.NullString
	ParentsCSV string // «Имя1, Имя2»
}

type ParentRow struct {
	ParentName string
	Children   string // «ФИО1, ФИО2»
	Classes    string // «7А, 11Б»
}

func ListAllUsersContext(ctx context.Context, database *sql.DB, includeInactive bool) ([]UserRow, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var q string
	if !includeInactive {
		q = `
		SELECT u.name, COALESCE(u.role, '') AS role, u.class_number, u.class_letter
		FROM users u
		WHERE u.confirmed = TRUE AND u.is_active=TRUE
		ORDER BY LOWER(u.name)`
	} else {
		q = `
		SELECT u.name, COALESCE(u.role, '') AS role, u.class_number, u.class_letter
		FROM users u
		WHERE TRUE
		ORDER BY LOWER(u.name)`
	}

	rows, err := database.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []UserRow
	for rows.Next() {
		var r UserRow
		if err := rows.Scan(&r.Name, &r.Role, &r.ClassNum, &r.ClassLet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func ListAllUsers(database *sql.DB, includeInactive bool) ([]UserRow, error) {
	return ListAllUsersContext(context.Background(), database, includeInactive)
}

func ListTeachersContext(ctx context.Context, database *sql.DB, includeInactive bool) ([]string, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	q := ""
	if !includeInactive {
		q = "SELECT u.name FROM users u WHERE u.role='teacher' AND u.confirmed=TRUE AND u.is_active=TRUE ORDER BY LOWER(u.name)"
	} else {
		q = "SELECT u.name FROM users u WHERE u.role='teacher' ORDER BY LOWER(u.name)"
	}
	rows, err := database.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var res []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		res = append(res, n)
	}
	return res, rows.Err()
}

func ListTeachers(database *sql.DB, includeInactive bool) ([]string, error) {
	return ListTeachersContext(context.Background(), database, includeInactive)
}

func ListAdministrationContext(ctx context.Context, database *sql.DB, includeInactive bool) ([]string, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	q := ""
	if !includeInactive {
		q = "SELECT u.name FROM users u WHERE u.role='administration' AND u.confirmed=TRUE AND u.is_active=TRUE ORDER BY LOWER(u.name)"
	} else {
		q = "SELECT u.name FROM users u WHERE u.role='administration' ORDER BY LOWER(u.name)"
	}
	rows, err := database.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var res []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		res = append(res, n)
	}
	return res, rows.Err()
}

func ListAdministration(database *sql.DB, includeInactive bool) ([]string, error) {
	return ListAdministrationContext(context.Background(), database, includeInactive)
}

func ListStudentsContext(ctx context.Context, database *sql.DB, includeInactive bool) ([]StudentRow, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	q := `
	SELECT
	u.name,
		u.class_number,
		u.class_letter,
		COALESCE(string_agg(p.name, ', ' ORDER BY LOWER(p.name)) FILTER (WHERE p.id IS NOT NULL), '') AS parents
	FROM users u
	LEFT JOIN parents_students ps ON ps.student_id = u.id
	LEFT JOIN users p ON p.id = ps.parent_id AND p.role = 'parent'`

	if !includeInactive {
		q += `
		WHERE u.role='student' AND u.confirmed=TRUE AND u.is_active=TRUE
		GROUP BY u.id
		ORDER BY COALESCE(u.class_number,0), u.class_letter, LOWER(u.name)
	`
	} else {
		q += `
		WHERE u.role='student'
		GROUP BY u.id
		ORDER BY COALESCE(u.class_number,0), u.class_letter, LOWER(u.name)
	`
	}
	rows, err := database.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var res []StudentRow
	for rows.Next() {
		var r StudentRow
		if err := rows.Scan(&r.Name, &r.ClassNum, &r.ClassLet, &r.ParentsCSV); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

func ListStudents(database *sql.DB, includeInactive bool) ([]StudentRow, error) {
	return ListStudentsContext(context.Background(), database, includeInactive)
}

func ListParentsContext(ctx context.Context, database *sql.DB, includeInactive bool) ([]ParentRow, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	q := `
		SELECT
			u.name AS parent_name,
			COALESCE(string_agg(s.name, ', ' ORDER BY LOWER(s.name)) FILTER (WHERE s.id IS NOT NULL), '') AS children,
			COALESCE(string_agg(
				CASE WHEN s.class_number IS NOT NULL AND s.class_letter IS NOT NULL
				     THEN concat(s.class_number::int, s.class_letter) ELSE '' END,
				', ' ORDER BY s.class_number, s.class_letter
			) FILTER (WHERE s.id IS NOT NULL), '') AS classes
		FROM users u
		LEFT JOIN parents_students ps ON ps.parent_id = u.id
		LEFT JOIN users s ON s.id = ps.student_id AND s.role = 'student'
	`
	if !includeInactive {
		q += `
		WHERE u.role='parent' AND u.confirmed=TRUE AND u.is_active=TRUE
		GROUP BY u.id
		ORDER BY LOWER(u.name)
	`
	} else {
		q += `
		WHERE u.role='parent'
		GROUP BY u.id
		ORDER BY LOWER(u.name)
	`
	}
	rows, err := database.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var res []ParentRow
	for rows.Next() {
		var r ParentRow
		if err := rows.Scan(&r.ParentName, &r.Children, &r.Classes); err != nil {
			return nil, err
		}
		r.Children = strings.Trim(r.Children, ", ")
		r.Classes = strings.Trim(r.Classes, ", ")
		res = append(res, r)
	}
	return res, rows.Err()
}

func ListParents(database *sql.DB, includeInactive bool) ([]ParentRow, error) {
	return ListParentsContext(context.Background(), database, includeInactive)
}
