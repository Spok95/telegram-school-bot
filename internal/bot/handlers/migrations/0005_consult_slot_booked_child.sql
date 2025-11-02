-- 0005_consult_slot_booked_child.sql

-- +goose Up
ALTER TABLE consult_slots
    ADD COLUMN IF NOT EXISTS booked_child_id BIGINT REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS booked_class_id INTEGER REFERENCES classes(id);

-- Добавляем ограничение целостности (миграция выполняется один раз — IF NOT EXISTS не нужен)
ALTER TABLE consult_slots
    ADD CONSTRAINT chk_slot_booking_coherence
        CHECK (
            (booked_by_id IS NULL AND booked_child_id IS NULL AND booked_class_id IS NULL)
                OR
            (booked_by_id IS NOT NULL AND booked_class_id IS NOT NULL)
            );

-- Индексы под выборки
CREATE INDEX IF NOT EXISTS ix_consult_booked_class ON consult_slots(booked_class_id, start_at) WHERE booked_by_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS ix_consult_booked_child ON consult_slots(booked_child_id) WHERE booked_by_id IS NOT NULL;

-- +goose Down
ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS chk_slot_booking_coherence;

ALTER TABLE consult_slots
    DROP COLUMN IF EXISTS booked_child_id,
    DROP COLUMN IF EXISTS booked_class_id;

DROP INDEX IF EXISTS ix_consult_booked_class;
DROP INDEX IF EXISTS ix_consult_booked_child;
