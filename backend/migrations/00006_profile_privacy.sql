-- +goose Up
ALTER TABLE users ADD COLUMN paused BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE user_blocks (
    blocker_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT chk_user_blocks_not_self CHECK (blocker_id <> blocked_id)
);
CREATE INDEX idx_user_blocks_blocked_id ON user_blocks (blocked_id);

-- +goose Down
DROP TABLE user_blocks;
ALTER TABLE users DROP COLUMN paused;
