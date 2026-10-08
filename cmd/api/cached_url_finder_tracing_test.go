package main

import (
	"context"
	"strings"
	"testing"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newCachedURLFinderTraceExporter(
	t *testing.T,
) *tracetest.InMemoryExporter {
	t.Helper()

	previousProvider := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)

	otel.SetTracerProvider(provider)

	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown trace provider: %v", err)
		}

		otel.SetTracerProvider(previousProvider)
	})

	return exporter
}

func findRecordedSpan(
	t *testing.T,
	exporter *tracetest.InMemoryExporter,
	name string,
) tracetest.SpanStub {
	t.Helper()

	var matches []tracetest.SpanStub

	for _, span := range exporter.GetSpans() {
		if span.Name == name {
			matches = append(matches, span)
		}
	}

	if len(matches) != 1 {
		t.Fatalf(
			"expected exactly one span %q, got %d",
			name,
			len(matches),
		)
	}

	return matches[0]
}

func recordedAttributeString(
	t *testing.T,
	span tracetest.SpanStub,
	key attribute.Key,
) string {
	t.Helper()

	for _, value := range span.Attributes {
		if value.Key == key {
			return value.Value.AsString()
		}
	}

	t.Fatalf(
		"span %q missing attribute %q",
		span.Name,
		key,
	)

	return ""
}

func recordedSpanNames(
	exporter *tracetest.InMemoryExporter,
) map[string]bool {
	names := make(map[string]bool)

	for _, span := range exporter.GetSpans() {
		names[span.Name] = true
	}

	return names
}

func TestCachedURLFinderTraceCacheHit(t *testing.T) {
	exporter := newCachedURLFinderTraceExporter(t)

	cache := &fakeRedirectCache{
		getValue: "https://example.com/cached",
	}
	source := &fakeURLStore{}

	finder := newTestCachedURLFinder(cache, source)

	if _, err := finder.GetURLByShortCode(
		context.Background(),
		"tracehit",
	); err != nil {
		t.Fatalf("resolve cached URL: %v", err)
	}

	resolveSpan := findRecordedSpan(
		t,
		exporter,
		"redirect.resolve",
	)

	cacheSpan := findRecordedSpan(
		t,
		exporter,
		"redirect.cache.get",
	)

	if got := recordedAttributeString(
		t,
		cacheSpan,
		"shortscale.cache.result",
	); got != redirectCacheResultHit {
		t.Fatalf(
			"expected cache result %q, got %q",
			redirectCacheResultHit,
			got,
		)
	}

	if got := recordedAttributeString(
		t,
		resolveSpan,
		"shortscale.resolve.source",
	); got != redirectResolveSourceCache {
		t.Fatalf(
			"expected resolve source %q, got %q",
			redirectResolveSourceCache,
			got,
		)
	}

	names := recordedSpanNames(exporter)

	if names["redirect.database.lookup"] {
		t.Fatal("database span must not exist on cache hit")
	}

	if names["redirect.cache.set"] {
		t.Fatal("cache-set span must not exist on cache hit")
	}

	for name := range names {
		if strings.Contains(name, "tracehit") {
			t.Fatalf(
				"short code leaked into span name %q",
				name,
			)
		}
	}
}

func TestCachedURLFinderTraceCacheMissDatabaseFallbackAndRefill(
	t *testing.T,
) {
	exporter := newCachedURLFinderTraceExporter(t)

	cache := &fakeRedirectCache{
		getErr: urlcache.ErrMiss,
	}
	source := &fakeURLStore{
		found: database.Url{
			ShortCode:   "tracemiss",
			OriginalUrl: "https://example.com/database",
		},
	}

	finder := newTestCachedURLFinder(cache, source)

	if _, err := finder.GetURLByShortCode(
		context.Background(),
		"tracemiss",
	); err != nil {
		t.Fatalf("resolve database URL: %v", err)
	}

	resolveSpan := findRecordedSpan(
		t,
		exporter,
		"redirect.resolve",
	)

	cacheGetSpan := findRecordedSpan(
		t,
		exporter,
		"redirect.cache.get",
	)

	databaseSpan := findRecordedSpan(
		t,
		exporter,
		"redirect.database.lookup",
	)

	cacheSetSpan := findRecordedSpan(
		t,
		exporter,
		"redirect.cache.set",
	)

	if got := recordedAttributeString(
		t,
		cacheGetSpan,
		"shortscale.cache.result",
	); got != redirectCacheResultMiss {
		t.Fatalf(
			"expected cache result %q, got %q",
			redirectCacheResultMiss,
			got,
		)
	}

	if got := recordedAttributeString(
		t,
		databaseSpan,
		"shortscale.database.result",
	); got != redirectDatabaseResultSuccess {
		t.Fatalf(
			"expected database result %q, got %q",
			redirectDatabaseResultSuccess,
			got,
		)
	}

	if got := recordedAttributeString(
		t,
		cacheSetSpan,
		"shortscale.cache.populate.result",
	); got != redirectCachePopulateResultSuccess {
		t.Fatalf(
			"expected cache population result %q, got %q",
			redirectCachePopulateResultSuccess,
			got,
		)
	}

	if got := recordedAttributeString(
		t,
		resolveSpan,
		"shortscale.resolve.source",
	); got != redirectResolveSourceDatabase {
		t.Fatalf(
			"expected resolve source %q, got %q",
			redirectResolveSourceDatabase,
			got,
		)
	}

	resolveSpanID := resolveSpan.SpanContext.SpanID()

	if databaseSpan.Parent.SpanID() != resolveSpanID {
		t.Fatal(
			"database lookup span must be a child of redirect.resolve",
		)
	}

	if cacheSetSpan.Parent.SpanID() != resolveSpanID {
		t.Fatal(
			"cache refill span must be a child of redirect.resolve",
		)
	}

	for name := range recordedSpanNames(exporter) {
		if strings.Contains(name, "tracemiss") {
			t.Fatalf(
				"short code leaked into span name %q",
				name,
			)
		}
	}
}
