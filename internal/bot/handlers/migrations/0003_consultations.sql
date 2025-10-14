-- +goose Up
CREATE TABLE IF NOT EXISTS consult_slots (
                                             id              bigserial PRIMARY KEY,
                                             teacher_id      bigint NOT NULL REFERENCES users(id),
                                             class_id        bigint NOT NULL REFERENCES classes(id),
                                             start_at        timestamptz NOT NULL,
                                             end_at          timestamptz NOT NULL,
                                             booked_by_id    bigint REFERENCES users(id),
                                             booked_at       timestamptz,
                                             cancel_reason   text,
                                             reminder_24h_sent boolean NOT NULL DEFAULT false,
                                             reminder_1h_sent  boolean NOT NULL DEFAULT false,
                                             created_at      timestamptz NOT NULL DEFAULT now(),
                                             updated_at      timestamptz NOT NULL DEFAULT now(),
                                             CHECK (end_at > start_at)
);

-- не допускаем пересечений у одного учителя по одинаковому старту
CREATE UNIQUE INDEX IF NOT EXISTS uq_consult_teacher_start ON consult_slots(teacher_id, start_at);

-- быстрый поиск свободных
CREATE INDEX IF NOT EXISTS ix_consult_free_class ON consult_slots(class_id, start_at) WHERE booked_by_id IS NULL;
CREATE INDEX IF NOT EXISTS ix_consult_free_teacher ON consult_slots(teacher_id, start_at) WHERE booked_by_id IS NULL;

-- +goose Down
DROP TABLE IF EXISTS consult_slots;
