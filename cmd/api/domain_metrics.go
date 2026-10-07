package main

import (
	"context"
	"errors"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"
)

const (
	cacheOperationGet = "get"
	cacheOperationSet = "set"

	cacheResultHit     = "hit"
	cacheResultMiss    = "miss"
	cacheResultSuccess = "success"
	cacheResultError   = "error"
)

type observedRedirectCache struct {
	next    redirectCache
	metrics *observability.Metrics
}

func newObservedRedirectCache(
	next redirectCache,
	metrics *observability.Metrics,
) redirectCache {
	if metrics == nil {
		return next
	}

	return &observedRedirectCache{
		next:    next,
		metrics: metrics,
	}
}

func (c *observedRedirectCache) Get(
	ctx context.Context,
	shortCode string,
) (string, error) {
	value, err := c.next.Get(ctx, shortCode)

	result := cacheResultHit

	switch {
	case errors.Is(err, urlcache.ErrMiss):
		result = cacheResultMiss
	case err != nil:
		result = cacheResultError
	}

	c.metrics.CacheOperationsTotal.
		WithLabelValues(
			cacheOperationGet,
			result,
		).
		Inc()

	return value, err
}

func (c *observedRedirectCache) Set(
	ctx context.Context,
	shortCode string,
	originalURL string,
) error {
	err := c.next.Set(
		ctx,
		shortCode,
		originalURL,
	)

	result := cacheResultSuccess
	if err != nil {
		result = cacheResultError
	}

	c.metrics.CacheOperationsTotal.
		WithLabelValues(
			cacheOperationSet,
			result,
		).
		Inc()

	return err
}
