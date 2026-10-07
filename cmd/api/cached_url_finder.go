package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"golang.org/x/sync/singleflight"
)

const (
	cacheOperationTimeout       = 50 * time.Millisecond
	sharedDatabaseLookupTimeout = 2 * time.Second
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
		cache:  cache,
		source: source,
	}
}

func (f *cachedURLFinder) GetURLByShortCode(
	ctx context.Context,
	shortCode string,
) (database.Url, error) {
	cacheCtx, cacheCancel := context.WithTimeout(ctx, cacheOperationTimeout)
	cachedURL, cacheErr := f.cache.Get(cacheCtx, shortCode)
	cacheCancel()

	if cacheErr == nil {
		return database.Url{
			ShortCode:   shortCode,
			OriginalUrl: cachedURL,
		}, nil
	}

	cacheReadFailed := !errors.Is(cacheErr, urlcache.ErrMiss)

	if cacheReadFailed {
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

			found, err := f.source.GetURLByShortCode(
				sharedCtx,
				shortCode,
			)
			if err != nil {
				return database.Url{}, err
			}

			if cacheReadFailed {
				return found, nil
			}

			cacheCtx, cacheCancel := context.WithTimeout(
				sharedCtx,
				cacheOperationTimeout,
			)
			defer cacheCancel()

			if err := f.cache.Set(
				cacheCtx,
				shortCode,
				found.OriginalUrl,
			); err != nil {
				f.logger.Warn(
					"failed to populate URL cache",
					"short_code", shortCode,
					"error", err,
				)
			}

			return found, nil
		},
	)

	select {
	case <-ctx.Done():
		return database.Url{}, ctx.Err()

	case result := <-resultCh:
		if result.Err != nil {
			return database.Url{}, result.Err
		}

		found, ok := result.Val.(database.Url)
		if !ok {
			return database.Url{}, errors.New(
				"unexpected URL lookup result type",
			)
		}

		return found, nil
	}
}
