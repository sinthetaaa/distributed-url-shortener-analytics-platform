package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	defaultAnalyticsDays = 7
	minAnalyticsDays     = 1
	maxAnalyticsDays     = 90
)

type redirectAnalyticsReader interface {
	GetURLByShortCode(context.Context, string) (database.Url, error)
	GetRedirectAnalyticsSummary(
		context.Context,
		string,
	) (database.GetRedirectAnalyticsSummaryRow, error)
	GetDailyRedirectCounts(
		context.Context,
		database.GetDailyRedirectCountsParams,
	) ([]database.GetDailyRedirectCountsRow, error)
}

type redirectAnalyticsResponse struct {
	ShortCode       string                          `json:"short_code"`
	TotalRedirects  int64                           `json:"total_redirects"`
	FirstRedirectAt *time.Time                      `json:"first_redirect_at"`
	LastRedirectAt  *time.Time                      `json:"last_redirect_at"`
	Window          redirectAnalyticsWindowResponse `json:"window"`
	Daily           []dailyRedirectCountResponse    `json:"daily"`
}

type redirectAnalyticsWindowResponse struct {
	Days      int       `json:"days"`
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	Redirects int64     `json:"redirects"`
}

type dailyRedirectCountResponse struct {
	Date      string `json:"date"`
	Redirects int64  `json:"redirects"`
}

func redirectAnalyticsHandler(
	logger *slog.Logger,
	reader redirectAnalyticsReader,
) http.HandlerFunc {
	return redirectAnalyticsHandlerWithClock(
		logger,
		reader,
		time.Now,
	)
}

func redirectAnalyticsHandlerWithClock(
	logger *slog.Logger,
	reader redirectAnalyticsReader,
	now func() time.Time,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shortCode := chi.URLParam(r, "shortCode")

		days, err := parseAnalyticsDays(r)
		if err != nil {
			writeJSONError(
				w,
				logger,
				http.StatusBadRequest,
				"days must be an integer between 1 and 90",
			)
			return
		}

		if _, err := reader.GetURLByShortCode(r.Context(), shortCode); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSONError(
					w,
					logger,
					http.StatusNotFound,
					"short URL not found",
				)
				return
			}

			logger.Error(
				"failed to verify short URL for analytics",
				"short_code", shortCode,
				"error", err,
			)
			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		summary, err := reader.GetRedirectAnalyticsSummary(
			r.Context(),
			shortCode,
		)
		if err != nil {
			logger.Error(
				"failed to load redirect analytics summary",
				"short_code", shortCode,
				"error", err,
			)
			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		windowStart, windowEnd := analyticsWindow(now(), days)

		dailyRows, err := reader.GetDailyRedirectCounts(
			r.Context(),
			database.GetDailyRedirectCountsParams{
				ShortCode: shortCode,
				StartAt: pgtype.Timestamptz{
					Time:  windowStart,
					Valid: true,
				},
				EndAt: pgtype.Timestamptz{
					Time:  windowEnd,
					Valid: true,
				},
			},
		)
		if err != nil {
			logger.Error(
				"failed to load daily redirect analytics",
				"short_code", shortCode,
				"error", err,
			)
			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		daily, windowRedirects, err := buildDailyRedirectCounts(
			windowStart,
			days,
			dailyRows,
		)
		if err != nil {
			logger.Error(
				"failed to shape daily redirect analytics",
				"short_code", shortCode,
				"error", err,
			)
			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		writeJSON(
			w,
			logger,
			http.StatusOK,
			redirectAnalyticsResponse{
				ShortCode:       shortCode,
				TotalRedirects:  summary.TotalRedirects,
				FirstRedirectAt: timestamptzPointer(summary.FirstRedirectAt),
				LastRedirectAt:  timestamptzPointer(summary.LastRedirectAt),
				Window: redirectAnalyticsWindowResponse{
					Days:      days,
					StartAt:   windowStart,
					EndAt:     windowEnd,
					Redirects: windowRedirects,
				},
				Daily: daily,
			},
		)
	}
}

func parseAnalyticsDays(r *http.Request) (int, error) {
	rawDays := r.URL.Query().Get("days")
	if rawDays == "" {
		return defaultAnalyticsDays, nil
	}

	days, err := strconv.Atoi(rawDays)
	if err != nil {
		return 0, err
	}

	if days < minAnalyticsDays || days > maxAnalyticsDays {
		return 0, strconv.ErrRange
	}

	return days, nil
}

func analyticsWindow(now time.Time, days int) (time.Time, time.Time) {
	nowUTC := now.UTC()

	todayStart := time.Date(
		nowUTC.Year(),
		nowUTC.Month(),
		nowUTC.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	endAt := todayStart.AddDate(0, 0, 1)
	startAt := endAt.AddDate(0, 0, -days)

	return startAt, endAt
}

func buildDailyRedirectCounts(
	startAt time.Time,
	days int,
	rows []database.GetDailyRedirectCountsRow,
) ([]dailyRedirectCountResponse, int64, error) {
	counts := make(map[string]int64, len(rows))
	var total int64

	for _, row := range rows {
		if !row.Day.Valid {
			return nil, 0, errors.New("daily analytics row has invalid date")
		}

		date := row.Day.Time.UTC().Format(time.DateOnly)
		counts[date] = row.Redirects
		total += row.Redirects
	}

	daily := make([]dailyRedirectCountResponse, 0, days)

	for offset := 0; offset < days; offset++ {
		date := startAt.AddDate(0, 0, offset).Format(time.DateOnly)

		daily = append(
			daily,
			dailyRedirectCountResponse{
				Date:      date,
				Redirects: counts[date],
			},
		)
	}

	return daily, total, nil
}

func timestamptzPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}

	valueUTC := value.Time.UTC()
	return &valueUTC
}
