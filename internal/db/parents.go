package db

import (
	"context"
	"database/sql"

	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
)

type ChildLite struct {
	ID       int64
	Name     string
	ClassID  sql.NullInt64
	ClassNum sql.NullInt64
	ClassLet sql.NullString
}

// ListChildrenForParent Дети родителя через parents_students
func ListChildrenForParent(ctx context.Context, database *sql.DB, parentID int64) ([]ChildLite, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

	rows, err := database.QueryContext(ctx, `
		SELECT u.id, u.name, u.class_id, u.class_number, u.class_letter
		FROM users u
		JOIN parents_students ps ON ps.student_id = u.id
		WHERE ps.parent_id = $1 AND u.is_active = TRUE
		ORDER BY u.name
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ChildLite
	for rows.Next() {
		var c ChildLite
		if err := rows.Scan(&c.ID, &c.Name, &c.ClassID, &c.ClassNum, &c.ClassLet); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListTeachersWithFutureSlotsByClass Учителя, у которых есть будущие слоты по указанному классу
func ListTeachersWithFutureSlotsByClass(ctx context.Context, database *sql.DB, classID int64, limit int) ([]TeacherLite, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

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
	defer func() { _ = rows.Close() }()

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
