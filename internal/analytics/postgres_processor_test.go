package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

type testRedirectEventStore struct {
	params       database.InsertRedirectEventParams
	rowsAffected int64
	err          error
	called       bool
}

func (s *testRedirectEventStore) InsertRedirectEvent(
	_ context.Context,
	params database.InsertRedirectEventParams,
) (int64, error) {
	s.called = true
	s.params = params

	return s.rowsAffected, s.err
}

func TestPostgresRedirectEventProcessorPersistsEvent(
	t *testing.T,
) {
	store := &testRedirectEventStore{
		rowsAffected: 1,
	}

	processor, err := NewPostgresRedirectEventProcessor(store)
	if err != nil {
		t.Fatalf("create processor: %v", err)
	}

	occurredAt := time.Date(
		2026,
		time.October,
		7,
		14,
		0,
		0,
		123000000,
		time.UTC,
	)

	event := RedirectEvent{
		EventID:    "123e4567-e89b-42d3-a456-426614174000",
		EventType:  RedirectEventType,
		ShortCode:  "3ZB9CeC",
		OccurredAt: occurredAt,
	}

	if err := processor.Process(context.Background(), event); err != nil {
		t.Fatalf("process redirect event: %v", err)
	}

	if !store.called {
		t.Fatal("expected InsertRedirectEvent to be called")
	}

	if store.params.EventID != event.EventID {
		t.Fatalf(
			"expected event id %q, got %q",
			event.EventID,
			store.params.EventID,
		)
	}

	if store.params.EventType != event.EventType {
		t.Fatalf(
			"expected event type %q, got %q",
			event.EventType,
			store.params.EventType,
		)
	}

	if store.params.ShortCode != event.ShortCode {
		t.Fatalf(
			"expected short code %q, got %q",
			event.ShortCode,
			store.params.ShortCode,
		)
	}

	if !store.params.OccurredAt.Valid {
		t.Fatal("expected occurred_at to be valid")
	}

	if !store.params.OccurredAt.Time.Equal(occurredAt) {
		t.Fatalf(
			"expected occurred_at %s, got %s",
			occurredAt,
			store.params.OccurredAt.Time,
		)
	}
}

func TestPostgresRedirectEventProcessorAcceptsDuplicate(
	t *testing.T,
) {
	store := &testRedirectEventStore{
		rowsAffected: 0,
	}

	processor, err := NewPostgresRedirectEventProcessor(store)
	if err != nil {
		t.Fatalf("create processor: %v", err)
	}

	if err := processor.Process(
		context.Background(),
		validRedirectEventForTest(),
	); err != nil {
		t.Fatalf(
			"expected duplicate event to be successful, got: %v",
			err,
		)
	}
}

func TestPostgresRedirectEventProcessorReturnsStoreFailure(
	t *testing.T,
) {
	store := &testRedirectEventStore{
		err: errors.New("database unavailable"),
	}

	processor, err := NewPostgresRedirectEventProcessor(store)
	if err != nil {
		t.Fatalf("create processor: %v", err)
	}

	if err := processor.Process(
		context.Background(),
		validRedirectEventForTest(),
	); err == nil {
		t.Fatal("expected persistence failure")
	}
}

func TestPostgresRedirectEventProcessorRejectsUnexpectedRowsAffected(
	t *testing.T,
) {
	store := &testRedirectEventStore{
		rowsAffected: 2,
	}

	processor, err := NewPostgresRedirectEventProcessor(store)
	if err != nil {
		t.Fatalf("create processor: %v", err)
	}

	if err := processor.Process(
		context.Background(),
		validRedirectEventForTest(),
	); err == nil {
		t.Fatal("expected unexpected row count to fail")
	}
}
