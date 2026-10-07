package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
)

type breakerCache struct {
	getCalls atomic.Int64
	setCalls atomic.Int64

	getValue string
	getErr   error
	setErr   error
}

func (c *breakerCache) Get(
	_ context.Context,
	_ string,
) (string, error) {
	c.getCalls.Add(1)

	return c.getValue, c.getErr
}

func (c *breakerCache) Set(
	_ context.Context,
	_ string,
	_ string,
) error {
	c.setCalls.Add(1)

	return c.setErr
}

func TestCacheCircuitBreakerOpensAfterFailureThreshold(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	cache := &breakerCache{
		getErr: redisErr,
	}

	breaker := newCacheCircuitBreaker(cache)

	for i := 0; i < cacheCircuitFailureThreshold; i++ {
		_, err := breaker.Get(
			context.Background(),
			"hot1234",
		)

		if !errors.Is(err, redisErr) {
			t.Fatalf(
				"expected Redis error on attempt %d, got %v",
				i+1,
				err,
			)
		}
	}

	_, err := breaker.Get(
		context.Background(),
		"hot1234",
	)

	if !errors.Is(err, errCacheCircuitOpen) {
		t.Fatalf(
			"expected open circuit after %d failures, got %v",
			cacheCircuitFailureThreshold,
			err,
		)
	}

	if calls := cache.getCalls.Load(); calls != cacheCircuitFailureThreshold {
		t.Fatalf(
			"expected %d underlying cache calls, got %d",
			cacheCircuitFailureThreshold,
			calls,
		)
	}
}

func TestCacheCircuitBreakerMissDoesNotCountAsFailure(t *testing.T) {
	cache := &breakerCache{
		getErr: urlcache.ErrMiss,
	}

	breaker := newCacheCircuitBreaker(cache)

	for i := 0; i < cacheCircuitFailureThreshold*2; i++ {
		_, err := breaker.Get(
			context.Background(),
			"missing",
		)

		if !errors.Is(err, urlcache.ErrMiss) {
			t.Fatalf(
				"expected cache miss, got %v",
				err,
			)
		}
	}

	if calls := cache.getCalls.Load(); calls != cacheCircuitFailureThreshold*2 {
		t.Fatalf(
			"expected every cache miss to reach Redis, got %d calls",
			calls,
		)
	}
}

func TestCacheCircuitBreakerSuccessfulRequestResetsFailures(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	cache := &breakerCache{
		getErr: redisErr,
	}

	breaker := newCacheCircuitBreaker(cache)

	for i := 0; i < cacheCircuitFailureThreshold-1; i++ {
		_, _ = breaker.Get(
			context.Background(),
			"hot1234",
		)
	}

	cache.getErr = nil
	cache.getValue = "https://example.com/hot"

	_, err := breaker.Get(
		context.Background(),
		"hot1234",
	)
	if err != nil {
		t.Fatalf("expected successful cache read, got %v", err)
	}

	cache.getErr = redisErr

	for i := 0; i < cacheCircuitFailureThreshold-1; i++ {
		_, err := breaker.Get(
			context.Background(),
			"hot1234",
		)

		if !errors.Is(err, redisErr) {
			t.Fatalf(
				"expected Redis error after reset, got %v",
				err,
			)
		}
	}

	_, err = breaker.Get(
		context.Background(),
		"hot1234",
	)

	if errors.Is(err, errCacheCircuitOpen) {
		t.Fatal(
			"expected success to reset previous failures before reopening threshold",
		)
	}
}

func TestCacheCircuitBreakerHalfOpenSuccessClosesCircuit(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	cache := &breakerCache{
		getErr: redisErr,
	}

	breaker := newCacheCircuitBreaker(cache)

	now := time.Date(
		2026,
		time.October,
		7,
		13,
		0,
		0,
		0,
		time.UTC,
	)

	breaker.now = func() time.Time {
		return now
	}

	for i := 0; i < cacheCircuitFailureThreshold; i++ {
		_, _ = breaker.Get(
			context.Background(),
			"hot1234",
		)
	}

	_, err := breaker.Get(
		context.Background(),
		"hot1234",
	)

	if !errors.Is(err, errCacheCircuitOpen) {
		t.Fatalf("expected circuit to be open, got %v", err)
	}

	now = now.Add(cacheCircuitOpenDuration)

	cache.getErr = nil
	cache.getValue = "https://example.com/hot"

	value, err := breaker.Get(
		context.Background(),
		"hot1234",
	)
	if err != nil {
		t.Fatalf("expected half-open probe to succeed, got %v", err)
	}

	if value != "https://example.com/hot" {
		t.Fatalf("unexpected value %q", value)
	}

	_, err = breaker.Get(
		context.Background(),
		"hot1234",
	)
	if err != nil {
		t.Fatalf(
			"expected circuit to remain closed after successful probe, got %v",
			err,
		)
	}

	if calls := cache.getCalls.Load(); calls != cacheCircuitFailureThreshold+2 {
		t.Fatalf(
			"expected successful probe and subsequent request to reach cache; got %d calls",
			calls,
		)
	}
}

func TestCacheCircuitBreakerHalfOpenFailureReopensCircuit(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	cache := &breakerCache{
		getErr: redisErr,
	}

	breaker := newCacheCircuitBreaker(cache)

	now := time.Date(
		2026,
		time.October,
		7,
		13,
		0,
		0,
		0,
		time.UTC,
	)

	breaker.now = func() time.Time {
		return now
	}

	for i := 0; i < cacheCircuitFailureThreshold; i++ {
		_, _ = breaker.Get(
			context.Background(),
			"hot1234",
		)
	}

	now = now.Add(cacheCircuitOpenDuration)

	_, err := breaker.Get(
		context.Background(),
		"hot1234",
	)

	if !errors.Is(err, redisErr) {
		t.Fatalf(
			"expected half-open Redis failure, got %v",
			err,
		)
	}

	_, err = breaker.Get(
		context.Background(),
		"hot1234",
	)

	if !errors.Is(err, errCacheCircuitOpen) {
		t.Fatalf(
			"expected failed probe to reopen circuit, got %v",
			err,
		)
	}
}

func TestCacheCircuitBreakerAllowsOnlyOneHalfOpenProbe(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	cache := &breakerCache{
		getErr: redisErr,
	}

	breaker := newCacheCircuitBreaker(cache)

	now := time.Date(
		2026,
		time.October,
		7,
		13,
		0,
		0,
		0,
		time.UTC,
	)

	breaker.now = func() time.Time {
		return now
	}

	for i := 0; i < cacheCircuitFailureThreshold; i++ {
		_, _ = breaker.Get(
			context.Background(),
			"hot1234",
		)
	}

	now = now.Add(cacheCircuitOpenDuration)

	// Hold the first permitted request in half-open state before it can
	// report success or failure.
	breaker.mu.Lock()

	if breaker.state != cacheCircuitOpen {
		breaker.mu.Unlock()
		t.Fatalf(
			"expected open circuit, got state %d",
			breaker.state,
		)
	}

	breaker.state = cacheCircuitHalfOpen
	breaker.mu.Unlock()

	const concurrentRequests = 25

	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	var rejected atomic.Int64

	for i := 0; i < concurrentRequests; i++ {
		go func() {
			defer wg.Done()

			_, err := breaker.Get(
				context.Background(),
				"hot1234",
			)

			if errors.Is(err, errCacheCircuitOpen) {
				rejected.Add(1)
			}
		}()
	}

	wg.Wait()

	if got := rejected.Load(); got != concurrentRequests {
		t.Fatalf(
			"expected all %d concurrent callers to be rejected while probe is active, got %d",
			concurrentRequests,
			got,
		)
	}
}
