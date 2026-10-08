-- name: InsertRedirectEvent :execrows
WITH inserted AS (
    INSERT INTO redirect_events (
        event_id,
        event_type,
        short_code,
        occurred_at
    )
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (event_id) DO NOTHING
    RETURNING
        short_code,
        occurred_at
)
INSERT INTO redirect_daily_counts (
    short_code,
    day,
    redirects
)
SELECT
    short_code,
    (occurred_at AT TIME ZONE 'UTC')::date,
    1
FROM inserted
ON CONFLICT (
    short_code,
    day
)
DO UPDATE SET
    redirects =
        redirect_daily_counts.redirects
        + EXCLUDED.redirects;

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
    day,
    redirects
FROM redirect_daily_counts
WHERE short_code = sqlc.arg(short_code)
  AND day >= (
      sqlc.arg(start_at)::timestamptz
      AT TIME ZONE 'UTC'
  )::date
  AND day < (
      sqlc.arg(end_at)::timestamptz
      AT TIME ZONE 'UTC'
  )::date
ORDER BY day;
