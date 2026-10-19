-- +goose Up

-- Older builds could persist external_id=0 when a messenger update omitted
-- user_id. Such rows cannot belong to a real account and are unsafe to reuse.
DELETE FROM bot_sessions WHERE external_id <= 0;
UPDATE users SET telegram_id = NULL WHERE telegram_id <= 0;
UPDATE users SET max_id = NULL WHERE max_id <= 0;

ALTER TABLE users
    ADD CONSTRAINT chk_users_telegram_id_positive
        CHECK (telegram_id IS NULL OR telegram_id > 0),
    ADD CONSTRAINT chk_users_max_id_positive
        CHECK (max_id IS NULL OR max_id > 0);

ALTER TABLE bot_sessions
    ADD CONSTRAINT chk_bot_sessions_external_id_positive
        CHECK (external_id > 0);

-- Feed lookup filters by university and then walks the primary-key order from
-- a random pivot. This index avoids sorting/scanning all users in that mode.
CREATE INDEX idx_users_university_id_id
    ON users (my_university_id, id);

-- +goose Down

DROP INDEX IF EXISTS idx_users_university_id_id;

ALTER TABLE bot_sessions
    DROP CONSTRAINT IF EXISTS chk_bot_sessions_external_id_positive;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS chk_users_max_id_positive,
    DROP CONSTRAINT IF EXISTS chk_users_telegram_id_positive;
