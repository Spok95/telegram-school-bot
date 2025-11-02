package db

import (
	"context"
	"database/sql"
	"time"

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

// ListTeachersWithSlotsByClassRange — учителя, у которых есть свободные слоты для classID в интервале [from,to).
// Учитывает consult_slots.class_id и consult_slot_classes.
func ListTeachersWithSlotsByClassRange(ctx context.Context, dbx *sql.DB, classID int64, from, to time.Time, limit int) ([]TeacherLite, error) {
	q := `
SELECT DISTINCT u.id, u.name
FROM users u
JOIN consult_slots s             ON s.teacher_id = u.id
LEFT JOIN consult_slot_classes csc ON csc.slot_id = s.id
WHERE u.role = 'teacher'
  AND s.booked_by_id IS NULL
  AND s.start_at >= $2 AND s.start_at < $3
  AND (
        s.class_id = $1
     OR csc.class_id = $1
  )
ORDER BY u.name
LIMIT $4`
	rows, err := dbx.QueryContext(ctx, q, classID, from.UTC(), to.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []TeacherLite
	for rows.Next() {
		var t TeacherLite
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTeachersWithSlotsByClassNLRange — то же, но класс задаётся номером и буквой.
func ListTeachersWithSlotsByClassNLRange(ctx context.Context, dbx *sql.DB, classNumber int, classLetter string, from, to time.Time, limit int) ([]TeacherLite, error) {
	q := `
WITH cls AS (
  SELECT id FROM classes WHERE number = $1 AND lower(letter) = lower($2) LIMIT 1
)
SELECT DISTINCT u.id, u.name
FROM users u
JOIN consult_slots s               ON s.teacher_id = u.id
LEFT JOIN consult_slot_classes csc ON csc.slot_id = s.id
WHERE u.role = 'teacher'
  AND s.booked_by_id IS NULL
  AND s.start_at >= $3 AND s.start_at < $4
  AND (
       s.class_id = (SELECT id FROM cls)
    OR csc.class_id = (SELECT id FROM cls)
  )
ORDER BY u.name
LIMIT $5`
	rows, err := dbx.QueryContext(ctx, q, classNumber, classLetter, from.UTC(), to.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []TeacherLite
	for rows.Next() {
		var t TeacherLite
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
