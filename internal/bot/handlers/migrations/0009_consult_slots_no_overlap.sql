-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- 1) Учитель: запрет пересечения любых интервалов у одного teacher_id
ALTER TABLE consult_slots
    ADD CONSTRAINT consult_slots_no_overlap_teacher
        EXCLUDE USING gist (
        teacher_id WITH =,
        tsrange(start_at, end_at, '[)') WITH &&
        );

-- 2) Родитель: запрет пересечения любых интервалов по booked_by_id
-- действует только на забронированные слоты
ALTER TABLE consult_slots
    ADD CONSTRAINT consult_slots_no_overlap_parent
        EXCLUDE USING gist (
        booked_by_id WITH =,
        tsrange(start_at, end_at, '[)') WITH &&
        )
        WHERE (booked_by_id IS NOT NULL);

-- +goose Down
ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS consult_slots_no_overlap_parent;

ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS consult_slots_no_overlap_teacher;
