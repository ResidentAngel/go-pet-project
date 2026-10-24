CREATE TABLE habits (
                        id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                        title       TEXT NOT NULL CHECK (length(title) > 0),
                        description TEXT,
                        created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE habit_logs (
                            id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                            habit_id BIGINT NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
                            done_on  DATE NOT NULL,
                            UNIQUE (habit_id, done_on)
);