-- +goose Up
ALTER TABLE users ADD COLUMN max_contact VARCHAR(512) NOT NULL DEFAULT '';
UPDATE users SET max_contact = max_profile_url WHERE max_profile_url <> '';
ALTER TABLE bot_sessions ADD COLUMN draft_contact VARCHAR(512) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE bot_sessions DROP COLUMN draft_contact;
ALTER TABLE users DROP COLUMN max_contact;
