-- +goose Up
ALTER TABLE bot_sessions ADD COLUMN peer_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE max_relays (
    message_id VARCHAR(128) PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_max_relays_created_at ON max_relays (created_at);

-- +goose Down
DROP TABLE max_relays;
ALTER TABLE bot_sessions DROP COLUMN peer_user_id;
