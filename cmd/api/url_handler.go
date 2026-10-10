package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/shortcode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	shortCodeLength        = 7
	maxShortCodeAttempts   = 5
	defaultURLListLimit    = 20
	maximumURLListLimit    = 100
	maxURLRequestBodyBytes = 4096
)

type urlCreator interface {
	CreateURL(context.Context, database.CreateURLParams) (database.Url, error)
}

type ownedURLCreator interface {
	CreateOwnedURL(
		context.Context,
		database.CreateOwnedURLParams,
	) (database.Url, error)
}

type userURLLister interface {
	ListURLsByUser(
		context.Context,
		database.ListURLsByUserParams,
	) ([]database.Url, error)
}

type urlFinder interface {
	GetURLByShortCode(context.Context, string) (database.Url, error)
}

type createURLRequest struct {
	URL string `json:"url"`
}

func decodeCreateURLRequest(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
) (createURLRequest, bool) {
	var request createURLRequest

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		maxURLRequestBodyBytes,
	)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeJSONError(
			w,
			logger,
			http.StatusBadRequest,
			"invalid request body",
		)
		return createURLRequest{}, false
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSONError(
			w,
			logger,
			http.StatusBadRequest,
			"invalid request body",
		)
		return createURLRequest{}, false
	}

	return request, true
}

type createURLResponse struct {
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
}

type userURLResponse struct {
	ShortCode   string     `json:"short_code"`
	OriginalURL string     `json:"original_url"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type listUserURLsResponse struct {
	URLs []userURLResponse `json:"urls"`
}

func listUserURLsHandler(
	logger *slog.Logger,
	lister userURLLister,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUserFromContext(r.Context())
		if !ok {
			logger.Error("authenticated user missing from URL list context")
			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		limit, err := parseURLListLimit(
			r.URL.Query().Get("limit"),
		)
		if err != nil {
			writeJSONError(
				w,
				logger,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		urls, err := lister.ListURLsByUser(
			r.Context(),
			database.ListURLsByUserParams{
				UserID: pgtype.Int8{
					Int64: user.ID,
					Valid: true,
				},
				Limit: int32(limit),
			},
		)
		if err != nil {
			logger.Error(
				"failed to list user URLs",
				"user_id",
				user.ID,
				"error",
				err,
			)

			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		response := make(
			[]userURLResponse,
			0,
			len(urls),
		)

		for _, item := range urls {
			var expiresAt *time.Time

			if item.ExpiresAt.Valid {
				expiry := item.ExpiresAt.Time
				expiresAt = &expiry
			}

			response = append(
				response,
				userURLResponse{
					ShortCode:   item.ShortCode,
					OriginalURL: item.OriginalUrl,
					CreatedAt:   item.CreatedAt.Time,
					ExpiresAt:   expiresAt,
				},
			)
		}

		writeJSON(
			w,
			logger,
			http.StatusOK,
			listUserURLsResponse{
				URLs: response,
			},
		)
	}
}

func parseURLListLimit(raw string) (int, error) {
	if raw == "" {
		return defaultURLListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf(
			"limit must be an integer between 1 and %d",
			maximumURLListLimit,
		)
	}

	if limit < 1 || limit > maximumURLListLimit {
		return 0, fmt.Errorf(
			"limit must be between 1 and %d",
			maximumURLListLimit,
		)
	}

	return limit, nil
}

func createOwnedURLHandler(
	logger *slog.Logger,
	creator ownedURLCreator,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUserFromContext(r.Context())
		if !ok {
			logger.Error("authenticated user missing from URL creation context")
			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		request, ok := decodeCreateURLRequest(
			w,
			r,
			logger,
		)
		if !ok {
			return
		}

		if err := validateOriginalURL(request.URL); err != nil {
			writeJSONError(
				w,
				logger,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		for attempt := 0; attempt < maxShortCodeAttempts; attempt++ {
			shortCode, err := shortcode.Generate(shortCodeLength)
			if err != nil {
				logger.Error(
					"failed to generate short code",
					"error",
					err,
				)

				writeJSONError(
					w,
					logger,
					http.StatusInternalServerError,
					"internal server error",
				)
				return
			}

			created, err := creator.CreateOwnedURL(
				r.Context(),
				database.CreateOwnedURLParams{
					ShortCode:   shortCode,
					OriginalUrl: request.URL,
					ExpiresAt: pgtype.Timestamptz{
						Valid: false,
					},
					UserID: pgtype.Int8{
						Int64: user.ID,
						Valid: true,
					},
				},
			)
			if err == nil {
				writeJSON(
					w,
					logger,
					http.StatusCreated,
					createURLResponse{
						ShortCode:   created.ShortCode,
						OriginalURL: created.OriginalUrl,
					},
				)
				return
			}

			if !isShortCodeCollision(err) {
				logger.Error(
					"failed to create owned URL",
					"user_id",
					user.ID,
					"error",
					err,
				)

				writeJSONError(
					w,
					logger,
					http.StatusInternalServerError,
					"internal server error",
				)
				return
			}
		}

		logger.Error(
			"failed to create owned URL after short-code collision retries",
			"user_id",
			user.ID,
		)

		writeJSONError(
			w,
			logger,
			http.StatusInternalServerError,
			"internal server error",
		)
	}
}

func createURLHandler(logger *slog.Logger, creator urlCreator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := decodeCreateURLRequest(
			w,
			r,
			logger,
		)
		if !ok {
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
