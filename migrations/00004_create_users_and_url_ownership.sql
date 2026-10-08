-- +goose Up
CREATE TABLE users (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_email_normalized_check CHECK (email = lower(email))
);

ALTER TABLE urls
ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT;

CREATE INDEX urls_user_id_created_at_idx
ON urls (user_id, created_at DESC, id DESC)
WHERE user_id IS NOT NULL;

CREATE TABLE user_sessions (
    token_hash BYTEA PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT user_sessions_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX user_sessions_user_id_idx
ON user_sessions (user_id);

CREATE INDEX user_sessions_expires_at_idx
ON user_sessions (expires_at);

-- +goose Down
DROP TABLE user_sessions;

DROP INDEX urls_user_id_created_at_idx;

ALTER TABLE urls
DROP COLUMN user_id;

DROP TABLE users;
