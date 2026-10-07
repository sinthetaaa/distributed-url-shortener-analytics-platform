package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func TestRedirectAnalyticsHandlerRejectsInvalidDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "zero",
			query: "?days=0",
		},
		{
			name:  "above maximum",
			query: "?days=91",
		},
		{
			name:  "not an integer",
			query: "?days=abc",
		},
		{
			name:  "negative",
			query: "?days=-1",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader := &stubRedirectAnalyticsReader{}

			router := newAnalyticsErrorTestRouter(reader)

			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/urls/abc1234/analytics"+test.query,
				nil,
			)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf(
					"expected status %d, got %d: %s",
					http.StatusBadRequest,
					response.Code,
					response.Body.String(),
				)
			}

			expectedBody := "{\"error\":\"days must be an integer between 1 and 90\"}\n"

			if response.Body.String() != expectedBody {
				t.Fatalf(
					"expected body %q, got %q",
					expectedBody,
					response.Body.String(),
				)
			}
		})
	}
}

func TestRedirectAnalyticsHandlerReturnsNotFoundForUnknownShortCode(
	t *testing.T,
) {
	t.Parallel()

	reader := &stubRedirectAnalyticsReader{
		urlErr: pgx.ErrNoRows,
	}

	router := newAnalyticsErrorTestRouter(reader)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/missing/analytics",
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusNotFound,
			response.Code,
			response.Body.String(),
		)
	}

	expectedBody := "{\"error\":\"short URL not found\"}\n"

	if response.Body.String() != expectedBody {
		t.Fatalf(
			"expected body %q, got %q",
			expectedBody,
			response.Body.String(),
		)
	}
}

func TestRedirectAnalyticsHandlerReturnsInternalErrorOnDatabaseFailure(
	t *testing.T,
) {
	t.Parallel()

	databaseFailure := errors.New("database unavailable")

	tests := []struct {
		name   string
		reader *stubRedirectAnalyticsReader
	}{
		{
			name: "URL existence lookup",
			reader: &stubRedirectAnalyticsReader{
				urlErr: databaseFailure,
			},
		},
		{
			name: "analytics summary",
			reader: &stubRedirectAnalyticsReader{
				url: database.Url{
					ShortCode: "abc1234",
				},
				summaryErr: databaseFailure,
			},
		},
		{
			name: "daily analytics",
			reader: &stubRedirectAnalyticsReader{
				url: database.Url{
					ShortCode: "abc1234",
				},
				summary:  database.GetRedirectAnalyticsSummaryRow{},
				dailyErr: databaseFailure,
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			router := newAnalyticsErrorTestRouter(test.reader)

			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/urls/abc1234/analytics",
				nil,
			)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusInternalServerError {
				t.Fatalf(
					"expected status %d, got %d: %s",
					http.StatusInternalServerError,
					response.Code,
					response.Body.String(),
				)
			}

			expectedBody := "{\"error\":\"internal server error\"}\n"

			if response.Body.String() != expectedBody {
				t.Fatalf(
					"expected body %q, got %q",
					expectedBody,
					response.Body.String(),
				)
			}
		})
	}
}

func newAnalyticsErrorTestRouter(
	reader redirectAnalyticsReader,
) http.Handler {
	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	fixedNow := time.Date(
		2026,
		time.October,
		7,
		15,
		30,
		0,
		0,
		time.UTC,
	)

	router := chi.NewRouter()

	router.Get(
		"/api/v1/urls/{shortCode}/analytics",
		redirectAnalyticsHandlerWithClock(
			logger,
			reader,
			func() time.Time {
				return fixedNow
			},
		),
	)

	return router
}
