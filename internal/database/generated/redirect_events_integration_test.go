package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInsertRedirectEventIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip(
			"DATABASE_URL is not set; skipping database integration test",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	queries := New(pool)

	eventID := fmt.Sprintf(
		"phase9h-integration-%d",
		time.Now().UnixNano(),
	)

	shortCode := fmt.Sprintf(
		"phase9h-%d",
		time.Now().UnixNano(),
	)

	occurredAt := time.Now().UTC().Truncate(time.Microsecond)

	params := InsertRedirectEventParams{
		EventID:   eventID,
		EventType: "redirect",
		ShortCode: shortCode,
		OccurredAt: pgtype.Timestamptz{
			Time:  occurredAt,
			Valid: true,
		},
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()

		if _, err := pool.Exec(
			cleanupCtx,
			"DELETE FROM redirect_events WHERE event_id = $1",
			eventID,
		); err != nil {
			t.Errorf("clean up redirect event: %v", err)
		}
	})

	rows, err := queries.InsertRedirectEvent(ctx, params)
	if err != nil {
		t.Fatalf("insert redirect event: %v", err)
	}

	if rows != 1 {
		t.Fatalf(
			"expected first insert to affect 1 row, got %d",
			rows,
		)
	}

	rows, err = queries.InsertRedirectEvent(ctx, params)
	if err != nil {
		t.Fatalf("insert duplicate redirect event: %v", err)
	}

	if rows != 0 {
		t.Fatalf(
			"expected duplicate insert to affect 0 rows, got %d",
			rows,
		)
	}

	found, err := queries.GetRedirectEventByID(ctx, eventID)
	if err != nil {
		t.Fatalf("get redirect event: %v", err)
	}

	if found.EventID != eventID {
		t.Fatalf(
			"expected event id %q, got %q",
			eventID,
			found.EventID,
		)
	}

	if found.ShortCode != shortCode {
		t.Fatalf(
			"expected short code %q, got %q",
			shortCode,
			found.ShortCode,
		)
	}

	if !found.OccurredAt.Valid {
		t.Fatal("expected occurred_at to be valid")
	}

	if !found.OccurredAt.Time.Equal(occurredAt) {
		t.Fatalf(
			"expected occurred_at %s, got %s",
			occurredAt,
			found.OccurredAt.Time,
		)
	}
}
