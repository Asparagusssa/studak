-- +goose Up
ALTER TABLE users ADD COLUMN telegram_photo_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN telegram_photo_id;
