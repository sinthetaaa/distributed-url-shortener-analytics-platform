package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/shortcode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	shortCodeLength      = 7
	maxShortCodeAttempts = 5
)

type urlCreator interface {
	CreateURL(context.Context, database.CreateURLParams) (database.Url, error)
}

type urlFinder interface {
	GetURLByShortCode(context.Context, string) (database.Url, error)
}

type createURLRequest struct {
	URL string `json:"url"`
}

type createURLResponse struct {
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
}

func createURLHandler(logger *slog.Logger, creator urlCreator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request createURLRequest

		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&request); err != nil {
			writeJSONError(w, logger, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := validateOriginalURL(request.URL); err != nil {
			writeJSONError(w, logger, http.StatusBadRequest, err.Error())
			return
		}

		for attempt := 0; attempt < maxShortCodeAttempts; attempt++ {
			shortCode, err := shortcode.Generate(shortCodeLength)
			if err != nil {
				logger.Error("failed to generate short code", "error", err)
				writeJSONError(w, logger, http.StatusInternalServerError, "internal server error")
				return
			}

			created, err := creator.CreateURL(r.Context(), database.CreateURLParams{
				ShortCode:   shortCode,
				OriginalUrl: request.URL,
				ExpiresAt:   pgtype.Timestamptz{Valid: false},
			})
			if err == nil {
				writeJSON(w, logger, http.StatusCreated, createURLResponse{
					ShortCode:   created.ShortCode,
					OriginalURL: created.OriginalUrl,
				})
				return
			}

			if !isShortCodeCollision(err) {
				logger.Error("failed to create URL", "error", err)
				writeJSONError(w, logger, http.StatusInternalServerError, "internal server error")
				return
			}
		}

		logger.Error("failed to create URL after short-code collision retries")
		writeJSONError(w, logger, http.StatusInternalServerError, "internal server error")
	}
}

func redirectURLHandler(logger *slog.Logger, finder urlFinder) http.HandlerFunc {
	return redirectURLHandlerWithEvents(
		logger,
		finder,
		noopRedirectEventRecorder{},
	)
}

func redirectURLHandlerWithEvents(
	logger *slog.Logger,
	finder urlFinder,
	recorder redirectEventRecorder,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shortCode := chi.URLParam(r, "shortCode")

		found, err := finder.GetURLByShortCode(r.Context(), shortCode)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSONError(w, logger, http.StatusNotFound, "short URL not found")
				return
			}

			logger.Error(
				"failed to find URL",
				"short_code", shortCode,
				"error", err,
			)
			writeJSONError(w, logger, http.StatusInternalServerError, "internal server error")
			return
		}

		http.Redirect(w, r, found.OriginalUrl, http.StatusFound)
		recordRedirectEvent(
			r.Context(),
			logger,
			recorder,
			shortCode,
		)
	}
}

func isShortCodeCollision(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == "urls_short_code_key"
}

func validateOriginalURL(rawURL string) error {
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("url must be a valid HTTP or HTTPS URL")
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("url must use http or https")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("url must include a host")
	}

	return nil
}

func writeJSONError(w http.ResponseWriter, logger *slog.Logger, statusCode int, message string) {
	writeJSON(w, logger, statusCode, map[string]string{
		"error": message,
	})
}

func writeJSON(w http.ResponseWriter, logger *slog.Logger, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Error("failed to encode JSON response", "error", err)
	}
}
