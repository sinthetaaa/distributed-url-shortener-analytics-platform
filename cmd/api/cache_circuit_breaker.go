package main

import (
	"context"
	"errors"
	"sync"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
)

var errCacheCircuitOpen = errors.New("cache circuit breaker open")

const (
	cacheCircuitFailureThreshold = 3
	cacheCircuitOpenDuration     = time.Second
)

type cacheCircuitState uint8

const (
	cacheCircuitClosed cacheCircuitState = iota
	cacheCircuitOpen
	cacheCircuitHalfOpen
)

type cacheCircuitBreaker struct {
	cache redirectCache

	mu                  sync.Mutex
	state               cacheCircuitState
	consecutiveFailures int
	openedAt            time.Time
	now                 func() time.Time
}

func newCacheCircuitBreaker(
	cache redirectCache,
) *cacheCircuitBreaker {
	return &cacheCircuitBreaker{
		cache: cache,
		now:   time.Now,
	}
}

func (b *cacheCircuitBreaker) Get(
	ctx context.Context,
	shortCode string,
) (string, error) {
	if !b.allowRequest() {
		return "", errCacheCircuitOpen
	}

	value, err := b.cache.Get(ctx, shortCode)

	switch {
	case err == nil:
		b.recordSuccess()

	case errors.Is(err, urlcache.ErrMiss):
		// A cache miss means Redis responded successfully.
		// It is not a dependency failure.
		b.recordSuccess()

	case errors.Is(err, context.Canceled):
		// Caller cancellation does not indicate Redis failure.
		return "", err

	default:
		b.recordFailure()
	}

	return value, err
}

func (b *cacheCircuitBreaker) Set(
	ctx context.Context,
	shortCode string,
	originalURL string,
) error {
	if !b.allowRequest() {
		return errCacheCircuitOpen
	}

	err := b.cache.Set(
		ctx,
		shortCode,
		originalURL,
	)

	switch {
	case err == nil:
		b.recordSuccess()

	case errors.Is(err, context.Canceled):
		// Caller cancellation does not indicate Redis failure.
		return err

	default:
		b.recordFailure()
	}

	return err
}

func (b *cacheCircuitBreaker) allowRequest() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case cacheCircuitClosed:
		return true

	case cacheCircuitOpen:
		if b.now().Sub(b.openedAt) < cacheCircuitOpenDuration {
			return false
		}

		// Exactly one request becomes the half-open probe.
		// Concurrent requests continue bypassing Redis.
		b.state = cacheCircuitHalfOpen

		return true

	case cacheCircuitHalfOpen:
		return false

	default:
		return false
	}
}

func (b *cacheCircuitBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = cacheCircuitClosed
	b.consecutiveFailures = 0
	b.openedAt = time.Time{}
}

func (b *cacheCircuitBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == cacheCircuitHalfOpen {
		b.open()
		return
	}

	if b.state == cacheCircuitOpen {
		return
	}

	b.consecutiveFailures++

	if b.consecutiveFailures >= cacheCircuitFailureThreshold {
		b.open()
	}
}

func (b *cacheCircuitBreaker) open() {
	b.state = cacheCircuitOpen
	b.consecutiveFailures = 0
	b.openedAt = b.now()
}
