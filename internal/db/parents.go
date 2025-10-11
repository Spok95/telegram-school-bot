package db

import (
	"context"
	"database/sql"
)

type ChildLite struct {
	ID       int64
	Name     string
	ClassID  sql.NullInt64
	ClassNum sql.NullInt64
	ClassLet sql.NullString
}

// ListChildrenForParent fallback-реализация: если есть users.child_id — вернем его как единственного "ребенка"
func ListChildrenForParent(ctx context.Context, database *sql.DB, parentID int64) ([]ChildLite, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT u2.id, u2.name, u2.class_id,
		       c.number, c.letter
		FROM users u
		LEFT JOIN users u2 ON u2.id = u.child_id
		LEFT JOIN classes c ON c.id = u2.class_id
		WHERE u.id = $1 AND u2.id IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChildLite
	for rows.Next() {
		var ch ChildLite
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.ClassID, &ch.ClassNum, &ch.ClassLet); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func ListTeachersWithFutureSlotsByClass(ctx context.Context, database *sql.DB, classID int64, limit int) ([]TeacherLite, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT u.id, u.name
		FROM consult_slots s
		JOIN users u ON u.id = s.teacher_id
		WHERE s.class_id = $1 AND s.start_at >= now()
		ORDER BY u.name
		LIMIT $2
	`, classID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []TeacherLite
	for rows.Next() {
		var t TeacherLite
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, rows.Err()
}
