-- +goose Up
ALTER TABLE users ADD COLUMN telegram_username VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN max_profile_url VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE bot_sessions ADD COLUMN messenger_username VARCHAR(64) NOT NULL DEFAULT '';
UPDATE bot_sessions SET step = 'menu', peer_user_id = NULL WHERE step = 'write';

-- +goose Down
ALTER TABLE bot_sessions DROP COLUMN messenger_username;
ALTER TABLE users DROP COLUMN max_profile_url;
ALTER TABLE users DROP COLUMN telegram_username;
