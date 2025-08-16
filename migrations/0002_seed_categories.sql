-- +goose Up
-- 1) Категории
INSERT INTO categories (name, label, is_active) VALUES
                                                    ('Работа на уроке',       'Работа на уроке',       TRUE),
                                                    ('Курсы по выбору',       'Курсы по выбору',       TRUE),
                                                    ('Внеурочная активность', 'Внеурочная активность', TRUE),
                                                    ('Социальные поступки',   'Социальные поступки',   TRUE),
                                                    ('Дежурство',             'Дежурство',             TRUE),
                                                    ('Аукцион',               'Аукцион',               TRUE)
    ON CONFLICT (name) DO NOTHING;

-- 2) Уровни 100/200/300
INSERT INTO score_levels (value, label, category_id)
SELECT v, lbl, c.id
FROM categories c
         JOIN (VALUES (100,'Базовый'), (200,'Средний'), (300,'Высокий')) AS L(v,lbl) ON TRUE
WHERE c.name IN ('Работа на уроке','Курсы по выбору','Внеурочная активность','Социальные поступки','Дежурство')
    ON CONFLICT (category_id, value) DO NOTHING;

-- 3) Классы 1–11 × А/Б/В/Г/Д
INSERT INTO classes (number, letter)
SELECT n, l
FROM generate_series(1,11) AS n
         CROSS JOIN (VALUES ('А'),('Б'),('В'),('Г'),('Д')) AS letters(l)
    ON CONFLICT (number, letter) DO NOTHING;

-- +goose Down
DELETE FROM score_levels WHERE value IN (100,200,300);
DELETE FROM categories WHERE name IN ('Работа на уроке','Курсы по выбору','Внеурочная активность','Социальные поступки','Дежурство','Аукцион');
DELETE FROM classes WHERE number BETWEEN 1 AND 11 AND letter IN ('А','Б','В','Г','Д');
