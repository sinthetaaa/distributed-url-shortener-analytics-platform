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

-- name: GetRedirectAnalyticsSummary :one
SELECT
    COUNT(*)::bigint AS total_redirects,
    MIN(occurred_at)::timestamptz AS first_redirect_at,
    MAX(occurred_at)::timestamptz AS last_redirect_at
FROM redirect_events
WHERE short_code = $1;

-- name: GetDailyRedirectCounts :many
SELECT
    (occurred_at AT TIME ZONE 'UTC')::date AS day,
    COUNT(*)::bigint AS redirects
FROM redirect_events
WHERE short_code = sqlc.arg(short_code)
  AND occurred_at >= sqlc.arg(start_at)
  AND occurred_at < sqlc.arg(end_at)
GROUP BY day
ORDER BY day;
