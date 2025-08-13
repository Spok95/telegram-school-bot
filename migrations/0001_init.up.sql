CREATE TABLE users (
                       id BIGSERIAL PRIMARY KEY,
                       telegram_id BIGINT UNIQUE NOT NULL,
                       name TEXT NOT NULL,
                       role TEXT NOT NULL CHECK (role IN ('student','parent','teacher','administration','admin')),
                       class_id BIGINT,
                       class_name TEXT,
                       class_number INT,
                       class_letter TEXT,
                       child_id BIGINT,
                       confirmed BOOLEAN NOT NULL DEFAULT FALSE,
                       is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE classes (
                         id BIGSERIAL PRIMARY KEY,
                         number INT NOT NULL,
                         letter TEXT NOT NULL,
                         collective_score INT NOT NULL DEFAULT 0,
                         UNIQUE (number, letter)
);

CREATE TABLE parents_students (
                                  parent_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                  student_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                  PRIMARY KEY (parent_id, student_id)
);

CREATE TABLE categories (
                            id BIGSERIAL PRIMARY KEY,
                            name  TEXT NOT NULL UNIQUE,
                            label TEXT NOT NULL
);

CREATE TABLE scores (
                        id BIGSERIAL PRIMARY KEY,
                        student_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        category_id BIGINT NOT NULL REFERENCES categories(id),
                        points      INT NOT NULL,
                        type        TEXT NOT NULL CHECK (type IN ('add','remove')),
                        comment     TEXT,
                        status      TEXT NOT NULL CHECK (status IN ('pending','approved','rejected')) DEFAULT 'pending',
                        approved_by BIGINT REFERENCES users(id),
                        approved_at TIMESTAMP,
                        created_by  BIGINT NOT NULL REFERENCES users(id),
                        created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE role_changes (
                              id BIGSERIAL PRIMARY KEY,
                              user_id   BIGINT NOT NULL REFERENCES users(id),
                              old_role  TEXT,
                              new_role  TEXT,
                              changed_by BIGINT NOT NULL REFERENCES users(id),
                              changed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE periods (
                         id BIGSERIAL PRIMARY KEY,
                         name TEXT NOT NULL,
                         start_date DATE NOT NULL,
                         end_date   DATE NOT NULL,
                         is_active  BOOLEAN NOT NULL DEFAULT FALSE
);

-- Флаг активности категорий (как в migrate.go)
ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;

-- Уровни баллов (как в migrate.go)
CREATE TABLE IF NOT EXISTS score_levels (
                                            id BIGSERIAL PRIMARY KEY,
                                            value INT NOT NULL,
                                            label TEXT NOT NULL,
                                            category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
                                            is_active BOOLEAN NOT NULL DEFAULT TRUE,
                                            UNIQUE (category_id, value)
);

-- Быстрый индекс уникальности (как в migrate.go)
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_levels_category_value
    ON score_levels(category_id, value);

-- Заявки на привязку родителя к ребёнку (как в migrate.go)
CREATE TABLE IF NOT EXISTS parent_link_requests (
                                                    id BIGSERIAL PRIMARY KEY,
                                                    parent_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                                    student_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                                    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);


-- Индексы
CREATE INDEX idx_scores_student_created ON scores(student_id, created_at);
CREATE INDEX idx_scores_category       ON scores(category_id);
CREATE INDEX idx_users_role            ON users(role);
CREATE UNIQUE INDEX idx_users_telegram ON users(telegram_id);
