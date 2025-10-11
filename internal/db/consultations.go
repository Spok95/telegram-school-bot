package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

type ConsultSlot struct {
	ID         int64
	TeacherID  int64
	ClassID    int64
	StartAt    time.Time
	EndAt      time.Time
	BookedByID sql.NullInt64
}

// CreateSlots — пачечная вставка готовых стартов слотов с длительностью dur.
// Конфликты по (teacher_id, start_at) игнорируются (idempotent).
func CreateSlots(ctx context.Context, database *sql.DB, teacherID, classID int64, starts []time.Time, dur time.Duration) (int, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO consult_slots (teacher_id, class_id, start_at, end_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (teacher_id, start_at) DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = stmt.Close() }()

	inserted := 0
	for _, s := range starts {
		if _, err := stmt.ExecContext(ctx, teacherID, classID, s, s.Add(dur)); err != nil {
			return 0, err
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// ListFreeSlots — свободные слоты (фильтры по классу/учителю опциональны), в окне [from, to).
func ListFreeSlots(ctx context.Context, database *sql.DB, classID, teacherID *int64, from, to time.Time, limit int) ([]ConsultSlot, error) {
	q := `
		SELECT id, teacher_id, class_id, start_at, end_at, booked_by_id
		FROM consult_slots
		WHERE booked_by_id IS NULL
		  AND start_at >= $1 AND start_at < $2
	`
	args := []any{from, to}
	idx := 3
	if classID != nil {
		q += fmt.Sprintf(" AND class_id = $%d", idx)
		args = append(args, *classID)
		idx++
	}
	if teacherID != nil {
		q += fmt.Sprintf(" AND teacher_id = $%d", idx)
		args = append(args, *teacherID)
		idx++
	}
	q += fmt.Sprintf(" ORDER BY start_at LIMIT $%d", idx)
	args = append(args, limit)

	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ConsultSlot, 0, limit)
	for rows.Next() {
		var s ConsultSlot
		if err := rows.Scan(&s.ID, &s.TeacherID, &s.ClassID, &s.StartAt, &s.EndAt, &s.BookedByID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TryBookSlot — атомарное бронирование: ставит booked_by_id, если слота ещё никто не занял.
func TryBookSlot(ctx context.Context, database *sql.DB, slotID, parentUserID int64) (bool, error) {
	res, err := database.ExecContext(ctx, `
		UPDATE consult_slots
		SET booked_by_id = $1, booked_at = now(), updated_at = now()
		WHERE id = $2 AND booked_by_id IS NULL
	`, parentUserID, slotID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// DueForReminder — слоты, по которым пора слать напоминания.
// intervalText: "24 hours" или "1 hours". batch — ограничение выборки.
func DueForReminder(ctx context.Context, database *sql.DB, intervalText string, batch int) ([]ConsultSlot, error) {
	is24h := intervalText == "24 hours"
	q := `
		SELECT id, teacher_id, class_id, start_at, end_at, booked_by_id
		FROM consult_slots
		WHERE booked_by_id IS NOT NULL
		  AND start_at BETWEEN now() + $1::interval - interval '1 minute'
		                    AND now() + $1::interval + interval '1 minute'
		  AND (CASE WHEN $2 THEN NOT reminder_24h_sent ELSE NOT reminder_1h_sent END)
		ORDER BY start_at
		LIMIT $3
	`
	rows, err := database.QueryContext(ctx, q, intervalText, is24h, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConsultSlot
	for rows.Next() {
		var s ConsultSlot
		if err := rows.Scan(&s.ID, &s.TeacherID, &s.ClassID, &s.StartAt, &s.EndAt, &s.BookedByID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkReminded — пометить, что напоминания отправлены.
func MarkReminded(ctx context.Context, database *sql.DB, ids []int64, intervalText string) error {
	is24h := intervalText == "24 hours"
	_, err := database.ExecContext(ctx, `
		UPDATE consult_slots
		SET reminder_24h_sent = reminder_24h_sent OR $1,
		    reminder_1h_sent  = reminder_1h_sent  OR $2,
		    updated_at = now()
		WHERE id = ANY($3)
	`, is24h, !is24h, pq.Array(ids))
	return err
}
