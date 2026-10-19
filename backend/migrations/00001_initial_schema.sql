-- +goose Up

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS universities (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    short_name VARCHAR(64),
    search_key VARCHAR(512)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_universities_name
    ON universities (name);
CREATE INDEX IF NOT EXISTS idx_universities_search_key
    ON universities (search_key);
CREATE INDEX IF NOT EXISTS idx_universities_search_key_trgm
    ON universities USING GIN (search_key gin_trgm_ops);

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT,
    max_id BIGINT,
    web_uuid VARCHAR(36) NOT NULL,
    name VARCHAR(100) NOT NULL,
    age BIGINT NOT NULL,
    gender VARCHAR(16),
    bio VARCHAR(1000),
    photo_url VARCHAR(512),
    my_university_id BIGINT,
    search_mode VARCHAR(16) NOT NULL DEFAULT 'all',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_users_age CHECK (age >= 16 AND age <= 100),
    CONSTRAINT chk_users_search_mode CHECK (search_mode IN ('my_only', 'selected', 'all')),
    CONSTRAINT fk_users_my_university
        FOREIGN KEY (my_university_id) REFERENCES universities (id)
        ON UPDATE CASCADE ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_telegram_id
    ON users (telegram_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_max_id
    ON users (max_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_web_uuid
    ON users (web_uuid);
CREATE INDEX IF NOT EXISTS idx_users_my_university_id
    ON users (my_university_id);

CREATE TABLE IF NOT EXISTS user_preferred_universities (
    user_id BIGINT NOT NULL,
    university_id BIGINT NOT NULL,
    PRIMARY KEY (user_id, university_id),
    CONSTRAINT fk_user_preferred_universities_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_user_preferred_universities_university
        FOREIGN KEY (university_id) REFERENCES universities (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_preferred_universities_university_id
    ON user_preferred_universities (university_id);

CREATE TABLE IF NOT EXISTS likes (
    id BIGSERIAL PRIMARY KEY,
    from_user_id BIGINT NOT NULL,
    to_user_id BIGINT NOT NULL,
    is_like BOOLEAN NOT NULL DEFAULT TRUE,
    is_mutual BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_likes_not_self CHECK (from_user_id <> to_user_id),
    CONSTRAINT fk_likes_from_user
        FOREIGN KEY (from_user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_likes_to_user
        FOREIGN KEY (to_user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_like_pair
    ON likes (from_user_id, to_user_id);
CREATE INDEX IF NOT EXISTS idx_likes_to_user_id
    ON likes (to_user_id);

CREATE TABLE IF NOT EXISTS bot_sessions (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(16) NOT NULL,
    external_id BIGINT NOT NULL,
    user_id BIGINT,
    step VARCHAR(32) NOT NULL,
    draft_name VARCHAR(100),
    draft_age BIGINT,
    draft_gender VARCHAR(16),
    draft_bio VARCHAR(1000),
    draft_university_id BIGINT,
    draft_search_mode VARCHAR(16),
    draft_preferred VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_bot_sessions_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT fk_bot_sessions_draft_university
        FOREIGN KEY (draft_university_id) REFERENCES universities (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_bot_sess
    ON bot_sessions (channel, external_id);
CREATE INDEX IF NOT EXISTS idx_bot_sessions_user_id
    ON bot_sessions (user_id);

-- +goose Down

DROP TABLE IF EXISTS bot_sessions;
DROP TABLE IF EXISTS likes;
DROP TABLE IF EXISTS user_preferred_universities;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS universities;
