-- +goose Up
CREATE TABLE redirect_daily_counts (
    short_code TEXT NOT NULL,
    day DATE NOT NULL,
    redirects BIGINT NOT NULL,
    PRIMARY KEY (short_code, day),
    CONSTRAINT redirect_daily_counts_redirects_check
        CHECK (redirects >= 0)
);

INSERT INTO redirect_daily_counts (
    short_code,
    day,
    redirects
)
SELECT
    short_code,
    (occurred_at AT TIME ZONE 'UTC')::date AS day,
    COUNT(*)::bigint AS redirects
FROM redirect_events
GROUP BY
    short_code,
    (occurred_at AT TIME ZONE 'UTC')::date;

-- +goose Down
DROP TABLE redirect_daily_counts;
