-- 1) Категории (точно как в seed.go)
INSERT INTO categories (name, label, is_active) VALUES
                                                    ('Работа на уроке',              'Работа на уроке',              TRUE),
                                                    ('Курсы по выбору',              'Курсы по выбору',              TRUE),
                                                    ('Внеурочная активность',        'Внеурочная активность',        TRUE),
                                                    ('Социальные поступки',          'Социальные поступки',          TRUE),
                                                    ('Дежурство',                    'Дежурство',                    TRUE),
                                                    ('Аукцион',                      'Аукцион',                      TRUE)
    ON CONFLICT (name) DO NOTHING;

-- 2) Уровни 100/200/300 с лейблами, как в seed.go (кроме "Аукцион")
-- (важно: НЕ использовать фиксированные id 1..5, а маппить по name)
INSERT INTO score_levels (value, label, category_id)
SELECT v, lbl, c.id
FROM categories c
         JOIN (
    VALUES (100, 'Базовый'),
           (200, 'Средний'),
           (300, 'Высокий')
) AS L(v, lbl) ON TRUE
WHERE c.name IN ('Работа на уроке','Курсы по выбору','Внеурочная активность','Социальные поступки','Дежурство')
    ON CONFLICT (category_id, value) DO NOTHING;

-- 3) Классы 1–11 × А/Б/В/Г/Д (как в seed.go), c защитой от дублей
INSERT INTO classes (number, letter)
SELECT n, l
FROM generate_series(1,11) AS n
         CROSS JOIN (VALUES ('А'),('Б'),('В'),('Г'),('Д')) AS letters(l)
    ON CONFLICT (number, letter) DO NOTHING;
