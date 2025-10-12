package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/models"
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

func ListTeachersWithSlotsByClassNLRange(
	ctx context.Context, database *sql.DB,
	classNumber int64, classLetter string,
	from, to time.Time, limit int,
) ([]TeacherLite, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	rows, err := database.QueryContext(ctx, `
        SELECT DISTINCT u.id, u.name
        FROM consult_slots s
        JOIN classes c ON c.id = s.class_id
        JOIN users   u ON u.id = s.teacher_id
        WHERE c.number = $1
          AND UPPER(c.letter) = UPPER($2)
          AND s.start_at >= $3 AND s.start_at < $4
        ORDER BY u.name
        LIMIT $5
    `, classNumber, classLetter, from.UTC(), to.UTC(), limit)
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

func ListTeachersWithSlotsByClassRange(ctx context.Context, database *sql.DB, classID int64, from, to time.Time, limit int) ([]TeacherLite, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	rows, err := database.QueryContext(ctx, `
        SELECT DISTINCT u.id, u.name
        FROM consult_slots s
        JOIN users u ON u.id = s.teacher_id
        WHERE s.class_id = $1
          AND s.start_at >= $2 AND s.start_at < $3
        ORDER BY u.name
        LIMIT $4
    `, classID, from.UTC(), to.UTC(), limit)
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

// GetChildByParentAndClassID Ребёнок родителя в конкретном классе (по class_id или по номеру/букве)
func GetChildByParentAndClassID(ctx context.Context, database *sql.DB, parentID, classID int64) (*models.User, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

	row := database.QueryRowContext(ctx, `
		SELECT u.id, u.telegram_id, u.name, u.class_id, u.class_number, u.class_letter
		FROM parents_students ps
		JOIN users u ON u.id = ps.student_id
		JOIN classes c ON c.id = $2
		WHERE ps.parent_id = $1
		  AND (
		    u.class_id = $2 OR
		    (u.class_id IS NULL AND u.class_number = c.number AND UPPER(u.class_letter) = UPPER(c.letter))
		  )
		LIMIT 1
	`, parentID, classID)

	var child models.User
	if err := row.Scan(&child.ID, &child.TelegramID, &child.Name, &child.ClassID, &child.ClassNumber, &child.ClassLetter); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &child, nil
}

// ListParentTelegramIDsByClass — все родительские telegram_id для класса (по class_id или номер/буква у детей)
func ListParentTelegramIDsByClass(ctx context.Context, database *sql.DB, classID int64) ([]int64, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT p.telegram_id
		FROM parents_students ps
		JOIN users p ON p.id = ps.parent_id
		JOIN users s ON s.id = ps.student_id AND s.role = 'student' AND s.is_active = TRUE
		JOIN classes c ON c.id = $1
		WHERE p.role = 'parent' AND p.confirmed = TRUE AND p.is_active = TRUE
		  AND p.telegram_id IS NOT NULL AND p.telegram_id <> 0
		  AND (
		    s.class_id = $1 OR
		    (s.class_id IS NULL AND s.class_number = c.number AND UPPER(s.class_letter) = UPPER(c.letter))
		  )
	`, classID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []int64
	for rows.Next() {
		var tgID int64
		if err := rows.Scan(&tgID); err != nil {
			return nil, err
		}
		out = append(out, tgID)
	}
	return out, rows.Err()
}
