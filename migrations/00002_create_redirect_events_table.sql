-- +goose Up
CREATE TABLE redirect_events (
    event_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    short_code TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT redirect_events_event_type_check
        CHECK (event_type = $$redirect$$)
);

CREATE INDEX redirect_events_short_code_occurred_at_idx
    ON redirect_events (short_code, occurred_at DESC);

-- +goose Down
DROP TABLE redirect_events;
