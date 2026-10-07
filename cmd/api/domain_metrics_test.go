package main

import (
	"context"
	"errors"
	"testing"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type stubDomainMetricsCache struct {
	value  string
	getErr error
	setErr error
}

func (c *stubDomainMetricsCache) Get(
	context.Context,
	string,
) (string, error) {
	return c.value, c.getErr
}

func (c *stubDomainMetricsCache) Set(
	context.Context,
	string,
	string,
) error {
	return c.setErr
}

func TestObservedRedirectCacheRecordsBoundedResults(t *testing.T) {
	t.Parallel()

	metrics, err := observability.NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	cache := &stubDomainMetricsCache{
		value: "https://example.com",
	}

	observed := newObservedRedirectCache(
		cache,
		metrics,
	)

	if _, err := observed.Get(
		context.Background(),
		"hit123",
	); err != nil {
		t.Fatalf("cache hit: %v", err)
	}

	cache.getErr = urlcache.ErrMiss

	if _, err := observed.Get(
		context.Background(),
		"miss123",
	); !errors.Is(err, urlcache.ErrMiss) {
		t.Fatalf(
			"expected cache miss, got %v",
			err,
		)
	}

	cache.getErr = errors.New("redis unavailable")

	if _, err := observed.Get(
		context.Background(),
		"err123",
	); err == nil {
		t.Fatal("expected cache get error")
	}

	cache.getErr = nil

	if err := observed.Set(
		context.Background(),
		"set123",
		"https://example.com",
	); err != nil {
		t.Fatalf("cache set success: %v", err)
	}

	cache.setErr = errors.New("redis unavailable")

	if err := observed.Set(
		context.Background(),
		"seterr",
		"https://example.com",
	); err == nil {
		t.Fatal("expected cache set error")
	}

	assertDomainMetricValue(
		t,
		metrics.CacheOperationsTotal.WithLabelValues(
			cacheOperationGet,
			cacheResultHit,
		),
		1,
	)

	assertDomainMetricValue(
		t,
		metrics.CacheOperationsTotal.WithLabelValues(
			cacheOperationGet,
			cacheResultMiss,
		),
		1,
	)

	assertDomainMetricValue(
		t,
		metrics.CacheOperationsTotal.WithLabelValues(
			cacheOperationGet,
			cacheResultError,
		),
		1,
	)

	assertDomainMetricValue(
		t,
		metrics.CacheOperationsTotal.WithLabelValues(
			cacheOperationSet,
			cacheResultSuccess,
		),
		1,
	)

	assertDomainMetricValue(
		t,
		metrics.CacheOperationsTotal.WithLabelValues(
			cacheOperationSet,
			cacheResultError,
		),
		1,
	)
}

func TestRedisTokenBucketLimiterRecordsBoundedDecisionResults(t *testing.T) {
	t.Parallel()

	metrics, err := observability.NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	limiter := &redisTokenBucketLimiter{
		metrics: metrics,
	}

	for _, result := range []string{
		rateLimitResultAllowed,
		rateLimitResultRejected,
		rateLimitResultFailOpen,
	} {
		limiter.recordDecision(result)

		assertDomainMetricValue(
			t,
			metrics.RateLimitDecisionsTotal.WithLabelValues(result),
			1,
		)
	}
}

func assertDomainMetricValue(
	t *testing.T,
	collector prometheus.Collector,
	want float64,
) {
	t.Helper()

	got := testutil.ToFloat64(collector)
	if got != want {
		t.Fatalf(
			"expected metric value %v, got %v",
			want,
			got,
		)
	}
}
