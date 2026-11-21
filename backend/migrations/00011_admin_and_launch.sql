-- +goose Up
ALTER TABLE users ADD COLUMN banned BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE bot_sessions ADD COLUMN admin_token VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE bot_sessions ADD COLUMN admin_action VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE bot_sessions ADD COLUMN admin_target BIGINT NOT NULL DEFAULT 0;
CREATE TABLE service_states (
 id BIGINT PRIMARY KEY CHECK (id = 1),
 started BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO service_states (id, started) VALUES (1, FALSE);

-- +goose Down
DROP TABLE service_states;
ALTER TABLE bot_sessions DROP COLUMN admin_target;
ALTER TABLE bot_sessions DROP COLUMN admin_action;
ALTER TABLE bot_sessions DROP COLUMN admin_token;
ALTER TABLE users DROP COLUMN banned;
