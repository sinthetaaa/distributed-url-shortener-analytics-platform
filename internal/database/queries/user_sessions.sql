-- name: CreateUserSession :one
INSERT INTO user_sessions (
    token_hash,
    user_id,
    expires_at
)
VALUES ($1, $2, $3)
RETURNING token_hash, user_id, created_at, expires_at;

-- name: GetUserSessionByTokenHash :one
SELECT token_hash, user_id, created_at, expires_at
FROM user_sessions
WHERE token_hash = $1
  AND expires_at > NOW();

-- name: DeleteUserSession :exec
DELETE FROM user_sessions
WHERE token_hash = $1;

-- name: DeleteExpiredUserSessions :execrows
DELETE FROM user_sessions
WHERE expires_at <= NOW();
