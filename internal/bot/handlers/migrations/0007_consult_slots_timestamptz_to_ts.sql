-- +goose Up
ALTER TABLE consult_slots
    ALTER COLUMN start_at TYPE timestamp without time zone
        USING start_at AT TIME ZONE 'UTC',
    ALTER COLUMN end_at   TYPE timestamp without time zone
        USING end_at   AT TIME ZONE 'UTC';

ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS chk_slot_booking_coherence;

-- логика:
-- 1) если слот свободен (booked_by_id/booked_at NULL) — позволяем любое cancel_reason
-- 2) если слот занят — требуем, чтобы и id, и время были заполнены
ALTER TABLE consult_slots
    ADD CONSTRAINT chk_slot_booking_coherence
        CHECK (
            (booked_by_id IS NULL AND booked_at IS NULL)
                OR
            (booked_by_id IS NOT NULL AND booked_at IS NOT NULL AND cancel_reason IS NULL)
            );

-- +goose Down
ALTER TABLE consult_slots
    ALTER COLUMN start_at TYPE timestamptz
        USING start_at AT TIME ZONE 'Europe/Moscow',
    ALTER COLUMN end_at   TYPE timestamptz
        USING end_at   AT TIME ZONE 'Europe/Moscow';
ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS chk_slot_booking_coherence;