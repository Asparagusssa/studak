-- +goose Up
ALTER TABLE users ADD COLUMN max_photo_token TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN max_photo_token;
