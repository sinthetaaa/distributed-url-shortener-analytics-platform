-- name: InsertRedirectEvent :execrows
INSERT INTO redirect_events (
    event_id,
    event_type,
    short_code,
    occurred_at
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (event_id) DO NOTHING;

-- name: GetRedirectEventByID :one
SELECT
    event_id,
    event_type,
    short_code,
    occurred_at,
    ingested_at
FROM redirect_events
WHERE event_id = $1;
