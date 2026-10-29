DROP INDEX habits_user_id_idx;
ALTER TABLE habits DROP COLUMN user_id;
DROP TABLE users;