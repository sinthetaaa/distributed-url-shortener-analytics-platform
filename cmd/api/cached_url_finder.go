package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

const cacheOperationTimeout = 50 * time.Millisecond

type redirectCache interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
}

type cachedURLFinder struct {
	logger *slog.Logger
	cache  redirectCache
	source urlFinder
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

	found, err := f.source.GetURLByShortCode(ctx, shortCode)
	if err != nil {
		return database.Url{}, err
	}

	if cacheReadFailed {
		return found, nil
	}

	cacheCtx, cacheCancel = context.WithTimeout(ctx, cacheOperationTimeout)
	cacheErr = f.cache.Set(cacheCtx, shortCode, found.OriginalUrl)
	cacheCancel()

	if cacheErr != nil {
		f.logger.Warn(
			"failed to populate URL cache",
			"short_code", shortCode,
			"error", cacheErr,
		)
	}

	return found, nil
}
