package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/sync/singleflight"
)

const (
	cacheOperationTimeout       = 50 * time.Millisecond
	sharedDatabaseLookupTimeout = 2 * time.Second

	redirectResolveResultSuccess  = "success"
	redirectResolveResultNotFound = "not_found"
	redirectResolveResultError    = "error"

	redirectResolveSourceCache    = "cache"
	redirectResolveSourceDatabase = "database"

	redirectCacheResultHit         = "hit"
	redirectCacheResultMiss        = "miss"
	redirectCacheResultError       = "error"
	redirectCacheResultCircuitOpen = "circuit_open"

	redirectDatabaseResultSuccess  = "success"
	redirectDatabaseResultNotFound = "not_found"
	redirectDatabaseResultError    = "error"

	redirectCachePopulateResultSuccess     = "success"
	redirectCachePopulateResultError       = "error"
	redirectCachePopulateResultCircuitOpen = "circuit_open"
	redirectCachePopulateResultSkipped     = "skipped"
)

type redirectCache interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
}

type cachedURLFinder struct {
	logger          *slog.Logger
	cache           redirectCache
	source          urlFinder
	databaseLookups singleflight.Group
}

func newCachedURLFinder(
	logger *slog.Logger,
	cache redirectCache,
	source urlFinder,
) *cachedURLFinder {
	return &cachedURLFinder{
		logger: logger,
		cache:  newCacheCircuitBreaker(cache),
		source: source,
	}
}

func (f *cachedURLFinder) GetURLByShortCode(
	ctx context.Context,
	shortCode string,
) (database.Url, error) {
	tracer := otel.Tracer(apiTracerName)

	ctx, resolveSpan := tracer.Start(
		ctx,
		"redirect.resolve",
	)
	defer resolveSpan.End()

	cacheCtx, cacheCancel := context.WithTimeout(
		ctx,
		cacheOperationTimeout,
	)
	cacheCtx, cacheSpan := tracer.Start(
		cacheCtx,
		"redirect.cache.get",
	)

	cachedURL, cacheErr := f.cache.Get(
		cacheCtx,
		shortCode,
	)

	cacheCancel()

	cacheMiss := errors.Is(cacheErr, urlcache.ErrMiss)
	circuitOpen := errors.Is(cacheErr, errCacheCircuitOpen)

	cacheResult := redirectCacheResultHit

	switch {
	case cacheErr == nil:
	case cacheMiss:
		cacheResult = redirectCacheResultMiss
	case circuitOpen:
		cacheResult = redirectCacheResultCircuitOpen
	default:
		cacheResult = redirectCacheResultError
		cacheSpan.RecordError(cacheErr)
		cacheSpan.SetStatus(codes.Error, "cache read failed")
	}

	cacheSpan.SetAttributes(
		attribute.String(
			"shortscale.cache.result",
			cacheResult,
		),
	)
	cacheSpan.End()

	if cacheErr == nil {
		resolveSpan.SetAttributes(
			attribute.String(
				"shortscale.resolve.result",
				redirectResolveResultSuccess,
			),
			attribute.String(
				"shortscale.resolve.source",
				redirectResolveSourceCache,
			),
		)

		return database.Url{
			ShortCode:   shortCode,
			OriginalUrl: cachedURL,
		}, nil
	}

	cacheReadFailed := !cacheMiss

	if cacheReadFailed && !circuitOpen {
		f.logger.Warn(
			"failed to read URL from cache; falling back to database",
			"short_code", shortCode,
			"error", cacheErr,
		)
	}

	resultCh := f.databaseLookups.DoChan(
		shortCode,
		func() (any, error) {
			detachedCtx := context.WithoutCancel(ctx)

			sharedCtx, sharedCancel := context.WithTimeout(
				detachedCtx,
				sharedDatabaseLookupTimeout,
			)
			defer sharedCancel()

			databaseCtx, databaseSpan := tracer.Start(
				sharedCtx,
				"redirect.database.lookup",
			)

			found, err := f.source.GetURLByShortCode(
				databaseCtx,
				shortCode,
			)

			switch {
			case err == nil:
				databaseSpan.SetAttributes(
					attribute.String(
						"shortscale.database.result",
						redirectDatabaseResultSuccess,
					),
				)

			case errors.Is(err, pgx.ErrNoRows):
				databaseSpan.SetAttributes(
					attribute.String(
						"shortscale.database.result",
						redirectDatabaseResultNotFound,
					),
				)

			default:
				databaseSpan.SetAttributes(
					attribute.String(
						"shortscale.database.result",
						redirectDatabaseResultError,
					),
				)
				databaseSpan.RecordError(err)
				databaseSpan.SetStatus(
					codes.Error,
					"database lookup failed",
				)
			}

			databaseSpan.End()

			if err != nil {
				return database.Url{}, err
			}

			if cacheReadFailed {
				resolveSpan.SetAttributes(
					attribute.String(
						"shortscale.cache.populate.result",
						redirectCachePopulateResultSkipped,
					),
				)

				return found, nil
			}

			cacheCtx, cacheCancel := context.WithTimeout(
				sharedCtx,
				cacheOperationTimeout,
			)
			defer cacheCancel()

			cacheCtx, cacheSetSpan := tracer.Start(
				cacheCtx,
				"redirect.cache.set",
			)

			cacheSetErr := f.cache.Set(
				cacheCtx,
				shortCode,
				found.OriginalUrl,
			)

			cachePopulateResult := redirectCachePopulateResultSuccess

			switch {
			case cacheSetErr == nil:
			case errors.Is(cacheSetErr, errCacheCircuitOpen):
				cachePopulateResult = redirectCachePopulateResultCircuitOpen
			default:
				cachePopulateResult = redirectCachePopulateResultError
				cacheSetSpan.RecordError(cacheSetErr)
				cacheSetSpan.SetStatus(
					codes.Error,
					"cache population failed",
				)
			}

			cacheSetSpan.SetAttributes(
				attribute.String(
					"shortscale.cache.populate.result",
					cachePopulateResult,
				),
			)
			cacheSetSpan.End()

			if cacheSetErr != nil &&
				!errors.Is(cacheSetErr, errCacheCircuitOpen) {
				f.logger.Warn(
					"failed to populate URL cache",
					"short_code", shortCode,
					"error", cacheSetErr,
				)
			}

			return found, nil
		},
	)

	select {
	case <-ctx.Done():
		resolveSpan.SetAttributes(
			attribute.String(
				"shortscale.resolve.result",
				redirectResolveResultError,
			),
		)
		resolveSpan.RecordError(ctx.Err())
		resolveSpan.SetStatus(
			codes.Error,
			"URL resolution cancelled",
		)

		return database.Url{}, ctx.Err()

	case result := <-resultCh:
		resolveSpan.SetAttributes(
			attribute.Bool(
				"shortscale.singleflight.shared",
				result.Shared,
			),
		)

		if result.Err != nil {
			if errors.Is(result.Err, pgx.ErrNoRows) {
				resolveSpan.SetAttributes(
					attribute.String(
						"shortscale.resolve.result",
						redirectResolveResultNotFound,
					),
				)

				return database.Url{}, result.Err
			}

			resolveSpan.SetAttributes(
				attribute.String(
					"shortscale.resolve.result",
					redirectResolveResultError,
				),
			)
			resolveSpan.RecordError(result.Err)
			resolveSpan.SetStatus(
				codes.Error,
				"URL resolution failed",
			)

			return database.Url{}, result.Err
		}

		found, ok := result.Val.(database.Url)
		if !ok {
			err := errors.New(
				"unexpected URL lookup result type",
			)

			resolveSpan.SetAttributes(
				attribute.String(
					"shortscale.resolve.result",
					redirectResolveResultError,
				),
			)
			resolveSpan.RecordError(err)
			resolveSpan.SetStatus(
				codes.Error,
				"URL resolution failed",
			)

			return database.Url{}, err
		}

		resolveSpan.SetAttributes(
			attribute.String(
				"shortscale.resolve.result",
				redirectResolveResultSuccess,
			),
			attribute.String(
				"shortscale.resolve.source",
				redirectResolveSourceDatabase,
			),
		)

		return found, nil
	}
}
