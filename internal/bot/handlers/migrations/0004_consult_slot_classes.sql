-- +goose Up
CREATE TABLE IF NOT EXISTS consult_slot_classes (
                                                    slot_id  bigint NOT NULL REFERENCES consult_slots(id) ON DELETE CASCADE,
                                                    class_id bigint NOT NULL REFERENCES classes(id),
                                                    PRIMARY KEY (slot_id, class_id)
);

-- backfill из старого поля class_id
INSERT INTO consult_slot_classes(slot_id, class_id)
SELECT id, class_id FROM consult_slots WHERE class_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- быстрый поиск свободных слотов по классу
CREATE INDEX IF NOT EXISTS ix_consult_slot_classes_by_class ON consult_slot_classes(class_id, slot_id);

-- оставляем consult_slots.class_id для обратной совместимости (fallback в запросах)

-- +goose Down
DROP TABLE IF EXISTS consult_slot_classes;
