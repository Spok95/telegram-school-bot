package db

import (
	"context"
	"database/sql"
	"errors"
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
	defer func() { _ = rows.Close() }()

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

// GetSlotByID — получить слот по id
func GetSlotByID(ctx context.Context, database *sql.DB, slotID int64) (*ConsultSlot, error) {
	row := database.QueryRowContext(ctx, `
		SELECT id, teacher_id, class_id, start_at, end_at, booked_by_id
		FROM consult_slots WHERE id = $1
	`, slotID)
	var s ConsultSlot
	if err := row.Scan(&s.ID, &s.TeacherID, &s.ClassID, &s.StartAt, &s.EndAt, &s.BookedByID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// ListFreeSlotsByTeacherOnDate — свободные слоты учителя на конкретный день (локальная дата)
func ListFreeSlotsByTeacherOnDate(ctx context.Context, database *sql.DB, teacherID int64, day time.Time, loc *time.Location, limit int) ([]ConsultSlot, error) {
	// начало и конец дня в локали → переводим в UTC
	startLocal := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	endLocal := startLocal.Add(24 * time.Hour)
	from := startLocal.UTC()
	to := endLocal.UTC()

	rows, err := database.QueryContext(ctx, `
		SELECT id, teacher_id, class_id, start_at, end_at, booked_by_id
		FROM consult_slots
		WHERE booked_by_id IS NULL
		  AND teacher_id = $1
		  AND start_at >= $2 AND start_at < $3
		ORDER BY start_at
		LIMIT $4
	`, teacherID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var res []ConsultSlot
	for rows.Next() {
		var s ConsultSlot
		if err := rows.Scan(&s.ID, &s.TeacherID, &s.ClassID, &s.StartAt, &s.EndAt, &s.BookedByID); err != nil {
			return nil, err
		}
		res = append(res, s)
	}
	return res, rows.Err()
}

// ListTeacherSlotsRange — слоты учителя в интервале [from,to), свободные и занятые
func ListTeacherSlotsRange(ctx context.Context, database *sql.DB, teacherID int64, from, to time.Time, limit int) ([]ConsultSlot, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id, teacher_id, class_id, start_at, end_at, booked_by_id
		FROM consult_slots
		WHERE teacher_id = $1 AND start_at >= $2 AND start_at < $3
		ORDER BY start_at
		LIMIT $4
	`, teacherID, from.UTC(), to.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var res []ConsultSlot
	for rows.Next() {
		var s ConsultSlot
		if err := rows.Scan(&s.ID, &s.TeacherID, &s.ClassID, &s.StartAt, &s.EndAt, &s.BookedByID); err != nil {
			return nil, err
		}
		res = append(res, s)
	}
	return res, rows.Err()
}

// DeleteFreeSlot — удалить свободный слот своего учителя
func DeleteFreeSlot(ctx context.Context, database *sql.DB, teacherID, slotID int64) (bool, error) {
	res, err := database.ExecContext(ctx, `
		DELETE FROM consult_slots
		WHERE id = $1 AND teacher_id = $2 AND booked_by_id IS NULL
	`, slotID, teacherID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// CancelBookedSlot — отменить занятую запись: возвращает ИД родителя, на которого была запись.
func CancelBookedSlot(ctx context.Context, database *sql.DB, teacherID, slotID int64, reason string) (parentID *int64, ok bool, err error) {
	var pid sql.NullInt64
	err = database.QueryRowContext(ctx, `
		WITH old AS (
			SELECT booked_by_id
			FROM consult_slots
			WHERE id = $2 AND teacher_id = $3 AND booked_by_id IS NOT NULL
			FOR UPDATE
		)
		UPDATE consult_slots s
		SET booked_by_id = NULL,
		    booked_at    = NULL,
		    cancel_reason= $1,
		    updated_at   = now()
		WHERE s.id = $2 AND s.teacher_id = $3 AND s.booked_by_id IS NOT NULL
		RETURNING (SELECT booked_by_id FROM old)
	`, reason, slotID, teacherID).Scan(&pid)

	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if pid.Valid {
		v := pid.Int64
		return &v, true, nil
	}
	return nil, true, nil
}
