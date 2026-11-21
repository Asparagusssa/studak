-- +goose Up
CREATE TABLE max_webhook_events (
    key VARCHAR(64) PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_max_webhook_events_created_at ON max_webhook_events (created_at);

-- +goose Down
DROP TABLE max_webhook_events;
