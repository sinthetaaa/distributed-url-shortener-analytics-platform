package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubRedirectAnalyticsReader struct {
	url         database.Url
	urlErr      error
	summary     database.GetRedirectAnalyticsSummaryRow
	summaryErr  error
	daily       []database.GetDailyRedirectCountsRow
	dailyErr    error
	dailyParams database.GetDailyRedirectCountsParams
}

func (s *stubRedirectAnalyticsReader) GetURLByShortCode(
	context.Context,
	string,
) (database.Url, error) {
	return s.url, s.urlErr
}

func (s *stubRedirectAnalyticsReader) GetRedirectAnalyticsSummary(
	context.Context,
	string,
) (database.GetRedirectAnalyticsSummaryRow, error) {
	return s.summary, s.summaryErr
}

func (s *stubRedirectAnalyticsReader) GetDailyRedirectCounts(
	_ context.Context,
	params database.GetDailyRedirectCountsParams,
) ([]database.GetDailyRedirectCountsRow, error) {
	s.dailyParams = params
	return s.daily, s.dailyErr
}

func TestRedirectAnalyticsHandlerDefaultWindow(t *testing.T) {
	t.Parallel()

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

	firstRedirect := time.Date(
		2026,
		time.September,
		30,
		9,
		15,
		0,
		0,
		time.UTC,
	)

	lastRedirect := time.Date(
		2026,
		time.October,
		7,
		14,
		45,
		0,
		0,
		time.UTC,
	)

	reader := &stubRedirectAnalyticsReader{
		url: database.Url{
			ShortCode: "abc1234",
		},
		summary: database.GetRedirectAnalyticsSummaryRow{
			TotalRedirects: 6,
			FirstRedirectAt: pgtype.Timestamptz{
				Time:  firstRedirect,
				Valid: true,
			},
			LastRedirectAt: pgtype.Timestamptz{
				Time:  lastRedirect,
				Valid: true,
			},
		},
		daily: []database.GetDailyRedirectCountsRow{
			{
				Day: pgtype.Date{
					Time: time.Date(
						2026,
						time.October,
						1,
						0,
						0,
						0,
						0,
						time.UTC,
					),
					Valid: true,
				},
				Redirects: 2,
			},
			{
				Day: pgtype.Date{
					Time: time.Date(
						2026,
						time.October,
						3,
						0,
						0,
						0,
						0,
						time.UTC,
					),
					Valid: true,
				},
				Redirects: 3,
			},
			{
				Day: pgtype.Date{
					Time: time.Date(
						2026,
						time.October,
						7,
						0,
						0,
						0,
						0,
						time.UTC,
					),
					Valid: true,
				},
				Redirects: 1,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

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

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/abc1234/analytics",
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusOK,
			response.Code,
			response.Body.String(),
		)
	}

	var body redirectAnalyticsResponse

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.ShortCode != "abc1234" {
		t.Fatalf("expected short code abc1234, got %q", body.ShortCode)
	}

	if body.TotalRedirects != 6 {
		t.Fatalf(
			"expected total redirects 6, got %d",
			body.TotalRedirects,
		)
	}

	if body.FirstRedirectAt == nil || !body.FirstRedirectAt.Equal(firstRedirect) {
		t.Fatalf(
			"unexpected first redirect: %v",
			body.FirstRedirectAt,
		)
	}

	if body.LastRedirectAt == nil || !body.LastRedirectAt.Equal(lastRedirect) {
		t.Fatalf(
			"unexpected last redirect: %v",
			body.LastRedirectAt,
		)
	}

	expectedStart := time.Date(
		2026,
		time.October,
		1,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	expectedEnd := time.Date(
		2026,
		time.October,
		8,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	if body.Window.Days != 7 {
		t.Fatalf("expected 7 days, got %d", body.Window.Days)
	}

	if !body.Window.StartAt.Equal(expectedStart) {
		t.Fatalf(
			"expected window start %s, got %s",
			expectedStart,
			body.Window.StartAt,
		)
	}

	if !body.Window.EndAt.Equal(expectedEnd) {
		t.Fatalf(
			"expected window end %s, got %s",
			expectedEnd,
			body.Window.EndAt,
		)
	}

	if body.Window.Redirects != 6 {
		t.Fatalf(
			"expected 6 window redirects, got %d",
			body.Window.Redirects,
		)
	}

	expectedCounts := []int64{2, 0, 3, 0, 0, 0, 1}

	if len(body.Daily) != len(expectedCounts) {
		t.Fatalf(
			"expected %d daily buckets, got %d",
			len(expectedCounts),
			len(body.Daily),
		)
	}

	for index, expectedCount := range expectedCounts {
		expectedDate := expectedStart.
			AddDate(0, 0, index).
			Format(time.DateOnly)

		if body.Daily[index].Date != expectedDate {
			t.Fatalf(
				"bucket %d: expected date %s, got %s",
				index,
				expectedDate,
				body.Daily[index].Date,
			)
		}

		if body.Daily[index].Redirects != expectedCount {
			t.Fatalf(
				"bucket %d: expected %d redirects, got %d",
				index,
				expectedCount,
				body.Daily[index].Redirects,
			)
		}
	}

	if !reader.dailyParams.StartAt.Valid ||
		!reader.dailyParams.StartAt.Time.Equal(expectedStart) {
		t.Fatalf(
			"unexpected query start: %+v",
			reader.dailyParams.StartAt,
		)
	}

	if !reader.dailyParams.EndAt.Valid ||
		!reader.dailyParams.EndAt.Time.Equal(expectedEnd) {
		t.Fatalf(
			"unexpected query end: %+v",
			reader.dailyParams.EndAt,
		)
	}
}

func TestRedirectAnalyticsHandlerZeroRedirects(t *testing.T) {
	t.Parallel()

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

	reader := &stubRedirectAnalyticsReader{
		url: database.Url{
			ShortCode: "zero123",
		},
		summary: database.GetRedirectAnalyticsSummaryRow{
			TotalRedirects: 0,
		},
		daily: nil,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

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

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/zero123/analytics?days=2",
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusOK,
			response.Code,
			response.Body.String(),
		)
	}

	var body redirectAnalyticsResponse

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.TotalRedirects != 0 {
		t.Fatalf(
			"expected zero total redirects, got %d",
			body.TotalRedirects,
		)
	}

	if body.FirstRedirectAt != nil {
		t.Fatalf(
			"expected null first redirect, got %v",
			body.FirstRedirectAt,
		)
	}

	if body.LastRedirectAt != nil {
		t.Fatalf(
			"expected null last redirect, got %v",
			body.LastRedirectAt,
		)
	}

	if body.Window.Days != 2 {
		t.Fatalf(
			"expected 2-day window, got %d",
			body.Window.Days,
		)
	}

	if body.Window.Redirects != 0 {
		t.Fatalf(
			"expected zero window redirects, got %d",
			body.Window.Redirects,
		)
	}

	if len(body.Daily) != 2 {
		t.Fatalf(
			"expected 2 daily buckets, got %d",
			len(body.Daily),
		)
	}

	for _, bucket := range body.Daily {
		if bucket.Redirects != 0 {
			t.Fatalf(
				"expected zero-filled bucket, got %+v",
				bucket,
			)
		}
	}
}
