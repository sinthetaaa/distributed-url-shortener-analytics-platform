-- name: CreateURL :one
INSERT INTO urls (
    short_code,
    original_url,
    expires_at
)
VALUES ($1, $2, $3)
RETURNING id, short_code, original_url, created_at, expires_at, user_id;

-- name: CreateOwnedURL :one
INSERT INTO urls (
    short_code,
    original_url,
    expires_at,
    user_id
)
VALUES ($1, $2, $3, $4)
RETURNING id, short_code, original_url, created_at, expires_at, user_id;

-- name: GetURLByShortCode :one
SELECT id, short_code, original_url, created_at, expires_at, user_id
FROM urls
WHERE short_code = $1;

-- name: GetURLByShortCodeForUser :one
SELECT id, short_code, original_url, created_at, expires_at, user_id
FROM urls
WHERE short_code = $1
  AND user_id = $2;

-- name: ListURLsByUser :many
SELECT id, short_code, original_url, created_at, expires_at, user_id
FROM urls
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;
