-- +goose Up
ALTER TABLE users ADD COLUMN search_gender VARCHAR(16) NOT NULL DEFAULT 'all'
    CHECK (search_gender IN ('all', 'female', 'male'));
ALTER TABLE bot_sessions ADD COLUMN draft_search_gender VARCHAR(16) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE bot_sessions DROP COLUMN draft_search_gender;
ALTER TABLE users DROP COLUMN search_gender;
