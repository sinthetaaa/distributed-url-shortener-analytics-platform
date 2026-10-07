package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type testRedirectFinder struct {
	found database.Url
	err   error
}

func (f testRedirectFinder) GetURLByShortCode(
	context.Context,
	string,
) (database.Url, error) {
	if f.err != nil {
		return database.Url{}, f.err
	}

	return f.found, nil
}

type testRedirectEventRecorder struct {
	events []analytics.RedirectEvent
}

func (r *testRedirectEventRecorder) Record(event analytics.RedirectEvent) {
	r.events = append(r.events, event)
}

func newRedirectAnalyticsTestRouter(
	finder urlFinder,
	recorder redirectEventRecorder,
) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := chi.NewRouter()
	router.Get(
		"/{shortCode}",
		redirectURLHandlerWithEvents(logger, finder, recorder),
	)

	return router
}

func TestSuccessfulRedirectRecordsAnalyticsEvent(t *testing.T) {
	recorder := &testRedirectEventRecorder{}
	router := newRedirectAnalyticsTestRouter(
		testRedirectFinder{
			found: database.Url{
				ShortCode:   "3ZB9CeC",
				OriginalUrl: "https://example.com/article",
			},
		},
		recorder,
	)

	request := httptest.NewRequest(http.MethodGet, "/3ZB9CeC", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusFound,
			response.Code,
		)
	}

	if len(recorder.events) != 1 {
		t.Fatalf(
			"expected exactly 1 redirect event, got %d",
			len(recorder.events),
		)
	}

	event := recorder.events[0]

	if event.EventID == "" {
		t.Fatal("expected event id to be set")
	}

	if event.EventType != analytics.RedirectEventType {
		t.Fatalf(
			"expected event type %q, got %q",
			analytics.RedirectEventType,
			event.EventType,
		)
	}

	if event.ShortCode != "3ZB9CeC" {
		t.Fatalf(
			"expected short code %q, got %q",
			"3ZB9CeC",
			event.ShortCode,
		)
	}

	if event.OccurredAt.IsZero() {
		t.Fatal("expected occurred_at to be set")
	}
}

func TestMissingRedirectDoesNotRecordAnalyticsEvent(t *testing.T) {
	recorder := &testRedirectEventRecorder{}
	router := newRedirectAnalyticsTestRouter(
		testRedirectFinder{err: pgx.ErrNoRows},
		recorder,
	)

	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusNotFound,
			response.Code,
		)
	}

	if len(recorder.events) != 0 {
		t.Fatalf(
			"expected no redirect events, got %d",
			len(recorder.events),
		)
	}
}

func TestFailedRedirectDoesNotRecordAnalyticsEvent(t *testing.T) {
	recorder := &testRedirectEventRecorder{}
	router := newRedirectAnalyticsTestRouter(
		testRedirectFinder{err: errors.New("database unavailable")},
		recorder,
	)

	request := httptest.NewRequest(http.MethodGet, "/3ZB9CeC", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusInternalServerError,
			response.Code,
		)
	}

	if len(recorder.events) != 0 {
		t.Fatalf(
			"expected no redirect events, got %d",
			len(recorder.events),
		)
	}
}
