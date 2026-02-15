-- +goose Up
ALTER TABLE consult_slots
    ADD COLUMN IF NOT EXISTS consult_format text NOT NULL DEFAULT 'offline',
    ADD COLUMN IF NOT EXISTS online_url text;

ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS chk_consult_format;

ALTER TABLE consult_slots
    ADD CONSTRAINT chk_consult_format
        CHECK (consult_format IN ('online','offline'));

-- (опционально) индекс, если планируете фильтровать по формату в выборках
-- CREATE INDEX IF NOT EXISTS ix_consult_format ON consult_slots(consult_format);

-- +goose Down
ALTER TABLE consult_slots
    DROP CONSTRAINT IF EXISTS chk_consult_format;

ALTER TABLE consult_slots
    DROP COLUMN IF EXISTS consult_format,
    DROP COLUMN IF EXISTS online_url;
