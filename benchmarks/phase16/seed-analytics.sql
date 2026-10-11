\set ON_ERROR_STOP on

BEGIN;

DELETE FROM redirect_daily_counts
WHERE short_code = :'code';

DELETE FROM redirect_events
WHERE short_code = :'code';

INSERT INTO redirect_events (
    event_id,
    event_type,
    short_code,
    occurred_at
)
SELECT
    'phase16-' || :'code' || '-' || lpad(gs::text, 6, '0'),
    'redirect',
    :'code',
    date_trunc('day', now())
        - interval '89 days'
        + (((gs - 1) % 90)::text || ' days')::interval
        + (((gs - 1) % 86400)::text || ' seconds')::interval
FROM generate_series(1, 100000) AS gs;

INSERT INTO redirect_daily_counts (
    short_code,
    day,
    redirects
)
SELECT
    short_code,
    (occurred_at AT TIME ZONE 'UTC')::date,
    COUNT(*)::bigint
FROM redirect_events
WHERE short_code = :'code'
GROUP BY
    short_code,
    (occurred_at AT TIME ZONE 'UTC')::date;

COMMIT;

SELECT COUNT(*)::bigint
FROM redirect_events
WHERE short_code = :'code';

SELECT COALESCE(SUM(redirects), 0)::bigint
FROM redirect_daily_counts
WHERE short_code = :'code';

WITH raw AS (
    SELECT
        (occurred_at AT TIME ZONE 'UTC')::date AS day,
        COUNT(*)::bigint AS redirects
    FROM redirect_events
    WHERE short_code = :'code'
    GROUP BY 1
),
rollup AS (
    SELECT
        day,
        redirects
    FROM redirect_daily_counts
    WHERE short_code = :'code'
)
SELECT COUNT(*)::bigint
FROM (
    SELECT
        COALESCE(raw.day, rollup.day) AS day,
        raw.redirects AS raw_redirects,
        rollup.redirects AS rollup_redirects
    FROM raw
    FULL OUTER JOIN rollup USING (day)
    WHERE raw.redirects IS DISTINCT FROM rollup.redirects
) AS mismatches;
