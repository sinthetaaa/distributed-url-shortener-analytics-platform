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
    COALESCE(
        (
            SELECT SUM(d.redirects)::bigint
            FROM redirect_daily_counts AS d
            WHERE d.short_code =
                sqlc.arg(target_short_code)
        ),
        0
    )::bigint AS total_redirects,
    (
        SELECT first_event.occurred_at
        FROM redirect_events AS first_event
        WHERE first_event.short_code =
            sqlc.arg(target_short_code)
        ORDER BY first_event.occurred_at ASC
        LIMIT 1
    )::timestamptz AS first_redirect_at,
    (
        SELECT last_event.occurred_at
        FROM redirect_events AS last_event
        WHERE last_event.short_code =
            sqlc.arg(target_short_code)
        ORDER BY last_event.occurred_at DESC
        LIMIT 1
    )::timestamptz AS last_redirect_at;

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
