-- +goose Up
-- Existing mutuality notifications already remain in messenger conversations.
DELETE FROM likes WHERE is_mutual = TRUE;
ALTER TABLE likes ADD COLUMN match_notified BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE likes DROP COLUMN match_notified;
