-- name: CreateURL :one
INSERT INTO urls (
    short_code,
    original_url,
    expires_at
)
VALUES ($1, $2, $3)
RETURNING id, short_code, original_url, created_at, expires_at;

-- name: GetURLByShortCode :one
SELECT id, short_code, original_url, created_at, expires_at
FROM urls
WHERE short_code = $1;
