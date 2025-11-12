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
		VALUES ($1, $2, $3::timestamp without time zone, $4::timestamp without time zone)
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
		SET booked_by_id  = $1,
		    booked_at     = NOW(),
		    cancel_reason = NULL,
		    updated_at    = NOW()
		WHERE id = $2
		  AND booked_by_id IS NULL
		  AND start_at > NOW()
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

	rows, err := database.QueryContext(ctx, `
		SELECT id, teacher_id, class_id, start_at, end_at, booked_by_id
		FROM consult_slots
		WHERE booked_by_id IS NULL
		  AND teacher_id = $1
		  AND start_at >= $2 AND start_at < $3
		ORDER BY start_at
		LIMIT $4
	`, teacherID, startLocal, endLocal, limit)
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

// CreateSlotsMultiClasses — создаёт/переиспользует слоты учителя на конкретных стартах
// и добавляет привязки слотов к нескольким классам (consult_slot_classes).
func CreateSlotsMultiClasses(ctx context.Context, dbx *sql.DB, teacherID int64, classIDs []int64, starts []time.Time, stepMin int) (int64, error) {
	if len(classIDs) == 0 || len(starts) == 0 {
		return 0, nil
	}

	tx, err := dbx.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	primaryClassID := classIDs[0]
	var processed int64

	for _, st := range starts {
		end := st.Add(time.Duration(stepMin) * time.Minute)

		// 1) пытаемся вставить слот: если уже есть такой (teacher_id, start_at) — не падаем
		var slotID int64
		err = tx.QueryRowContext(ctx, `
			INSERT INTO consult_slots (teacher_id, class_id, start_at, end_at)
			VALUES ($1, $2, $3::timestamp without time zone, $4::timestamp without time zone)
			ON CONFLICT (teacher_id, start_at) DO NOTHING
			RETURNING id
		`, teacherID, primaryClassID, st, end).Scan(&slotID)

		if err == sql.ErrNoRows {
			// слот уже существовал — возьмём его id
			err = tx.QueryRowContext(ctx, `
				SELECT id FROM consult_slots
				WHERE teacher_id = $1 AND start_at = $2
				LIMIT 1
			`, teacherID, st).Scan(&slotID)
		}
		if err != nil {
			return 0, err
		}

		// 2) привязываем слот ко всем выбранным классам (включая primary)
		for _, cid := range classIDs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO consult_slot_classes (slot_id, class_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, slotID, cid); err != nil {
				return 0, err
			}
		}

		processed++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return processed, nil
}

// ListDaysWithFreeSlotsByTeacherForClass — даты (YYYY-MM-DD) cо свободными слотами для данного учителя и класса в интервале.
func ListDaysWithFreeSlotsByTeacherForClass(ctx context.Context, database *sql.DB, teacherID, classID int64, from, to time.Time, limit int) ([]time.Time, error) {
	rows, err := database.QueryContext(ctx, `
	    SELECT DISTINCT
	        s.start_at::date AS day_local
	    FROM consult_slots s
	    WHERE s.booked_by_id IS NULL
	      AND s.teacher_id = $1
	      AND s.start_at >= $2 AND s.start_at < $3
	      AND (
	            s.class_id = $4
	         OR EXISTS (
	              SELECT 1
	              FROM consult_slot_classes csc
	              WHERE csc.slot_id = s.id AND csc.class_id = $4
	         )
	      )
	    ORDER BY day_local
	    LIMIT $5
	`, teacherID, from, to, classID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		// d уже «дата без времени», оставляем как есть
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListFreeSlotsByTeacherOnDateForClass — свободные слоты на конкретную дату (с учётом consult_slot_classes).
func ListFreeSlotsByTeacherOnDateForClass(ctx context.Context, database *sql.DB, teacherID, classID int64, day time.Time, loc *time.Location, limit int) ([]ConsultSlot, error) {
	startLocal := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	endLocal := startLocal.Add(24 * time.Hour)

	rows, err := database.QueryContext(ctx, `
	SELECT s.id, s.teacher_id, s.class_id, s.start_at, s.end_at, s.booked_by_id
	FROM consult_slots s
	WHERE s.booked_by_id IS NULL
	  AND s.teacher_id = $1
	  AND s.start_at >= $2 AND s.start_at < $3
	  AND (
	       s.class_id = $4
	       OR EXISTS (
	           SELECT 1 FROM consult_slot_classes csc
	           WHERE csc.slot_id = s.id AND csc.class_id = $4
	       )
	  )
	ORDER BY s.start_at
	LIMIT $5
`, teacherID, startLocal, endLocal, classID, limit)
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

// ParentCancelBookedSlot — родитель отменяет свою запись.
func ParentCancelBookedSlot(ctx context.Context, database *sql.DB, parentID, slotID int64, reason string) (ok bool, err error) {
	res, err := database.ExecContext(ctx, `
		UPDATE consult_slots
		SET booked_by_id = NULL,
		    booked_at    = NULL,
		    cancel_reason= $1,
		    updated_at   = now()
		WHERE id = $2 AND booked_by_id = $3
	`, reason, slotID, parentID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

type ParentBooking struct {
	SlotID     int64
	StartAt    time.Time
	EndAt      time.Time
	TeacherID  int64
	Teacher    string
	ClassID    int64
	ClassLabel string
	ChildName  string
}

// ListParentBookings — предстоящие записи родителя (на будущее).
func ListParentBookings(ctx context.Context, database *sql.DB, parentID int64, from time.Time, limit int) ([]ParentBooking, error) {
	// Берём имя ребёнка через parents_students -> users (как в Excel-экспорте)
	q := `
		SELECT s.id, s.start_at, s.end_at,
		       t.id, t.name,
		       COALESCE(cls.id, 0),
		       COALESCE(cls.number::text || cls.letter, ''),
		       COALESCE(ch.name, '')
		FROM consult_slots s
		JOIN users t ON t.id = s.teacher_id
		LEFT JOIN classes cls ON cls.id = s.class_id
		LEFT JOIN LATERAL (
			SELECT u.name
			FROM parents_students ps
			JOIN users u ON u.id = ps.student_id
			WHERE ps.parent_id = s.booked_by_id
			  AND (
			     (cls.id IS NOT NULL AND u.class_id = cls.id)
			     OR (cls.id IS NULL AND u.class_id IS NULL
			         AND u.class_number IS NOT NULL AND u.class_letter IS NOT NULL
			         AND cls.id IS NULL) -- fallback не нужен, но оставим структуру на будущее
			  )
			LIMIT 1
		) ch ON TRUE
		WHERE s.booked_by_id = $1
		  AND s.start_at >= $2
		ORDER BY s.start_at
		LIMIT $3`
	rows, err := database.QueryContext(ctx, q, parentID, from, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	loc := time.Local
	var out []ParentBooking
	for rows.Next() {
		var r ParentBooking
		if err := rows.Scan(&r.SlotID, &r.StartAt, &r.EndAt, &r.TeacherID, &r.Teacher, &r.ClassID, &r.ClassLabel, &r.ChildName); err != nil {
			return nil, err
		}

		r.StartAt = time.Date(
			r.StartAt.Year(), r.StartAt.Month(), r.StartAt.Day(),
			r.StartAt.Hour(), r.StartAt.Minute(), r.StartAt.Second(), r.StartAt.Nanosecond(),
			loc,
		)
		r.EndAt = time.Date(
			r.EndAt.Year(), r.EndAt.Month(), r.EndAt.Day(),
			r.EndAt.Hour(), r.EndAt.Minute(), r.EndAt.Second(), r.EndAt.Nanosecond(),
			loc,
		)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SlotHasClass — проверяет, разрешён ли слот slotID для указанного classID
// (или как основной класс слота, или через consult_slot_classes).
func SlotHasClass(ctx context.Context, database *sql.DB, slotID, classID int64) (bool, error) {
	var ok bool
	err := database.QueryRowContext(ctx, `
		SELECT 
		  (EXISTS (SELECT 1 FROM consult_slots s WHERE s.id = $1 AND s.class_id = $2))
		  OR
		  (EXISTS (SELECT 1 FROM consult_slot_classes csc WHERE csc.slot_id = $1 AND csc.class_id = $2))
	`, slotID, classID).Scan(&ok)
	return ok, err
}

// TryBookSlotWithChild — атомарно бронирует слот и фиксирует, какого ребёнка и класс выбрали.
// Возвращает true, если удалось (слот был свободен).
func TryBookSlotWithChild(ctx context.Context, dbx *sql.DB, slotID, parentID, childID, classID int64) (bool, error) {
	res, err := dbx.ExecContext(ctx, `
	UPDATE consult_slots
	SET booked_by_id    = $2,
	    booked_at       = NOW(),
	    booked_child_id = $3,
	    booked_class_id = $4,
	    cancel_reason   = NULL,
	    updated_at      = NOW()
	WHERE id = $1
	  AND booked_by_id IS NULL
	  AND start_at > NOW()
`, slotID, parentID, childID, classID)
	if err != nil {
		return false, err
	}
	aff, _ := res.RowsAffected()
	return aff > 0, nil
}
