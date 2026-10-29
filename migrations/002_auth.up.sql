CREATE TABLE users (
                       id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                       email         TEXT NOT NULL CHECK (email <> ''),
                       password_hash TEXT NOT NULL,
                       created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

-- привычки без владельца больше не нужны (в базе только тестовые данные),
-- вместе с ними через ON DELETE CASCADE удалятся и отметки
DELETE FROM habits;

ALTER TABLE habits
    ADD COLUMN user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX habits_user_id_idx ON habits (user_id);