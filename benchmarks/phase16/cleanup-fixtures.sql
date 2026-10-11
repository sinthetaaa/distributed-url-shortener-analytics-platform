\set ON_ERROR_STOP on

BEGIN;

DELETE FROM redirect_daily_counts
WHERE short_code IN (
    :'redirect_code',
    :'analytics_code',
    :'async_code'
);

DELETE FROM redirect_events
WHERE short_code IN (
    :'redirect_code',
    :'analytics_code',
    :'async_code'
);

DELETE FROM urls
WHERE short_code IN (
    :'redirect_code',
    :'analytics_code',
    :'async_code'
);

DELETE FROM users
WHERE email = :'email';

COMMIT;
