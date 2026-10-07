package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5"
)

type fakeRedirectCache struct {
	getValue  string
	getErr    error
	getCode   string
	getCalled bool

	setCode   string
	setURL    string
	setErr    error
	setCalled bool
}

func (f *fakeRedirectCache) Get(
	_ context.Context,
	shortCode string,
) (string, error) {
	f.getCalled = true
	f.getCode = shortCode

	if f.getErr != nil {
		return "", f.getErr
	}

	return f.getValue, nil
}

func (f *fakeRedirectCache) Set(
	_ context.Context,
	shortCode string,
	originalURL string,
) error {
	f.setCalled = true
	f.setCode = shortCode
	f.setURL = originalURL

	return f.setErr
}

func newTestCachedURLFinder(
	cache redirectCache,
	source urlFinder,
) *cachedURLFinder {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newCachedURLFinder(logger, cache, source)
}

func TestCachedURLFinderCacheHit(t *testing.T) {
	cache := &fakeRedirectCache{
		getValue: "https://example.com/cached",
	}
	source := &fakeURLStore{}

	finder := newTestCachedURLFinder(cache, source)

	found, err := finder.GetURLByShortCode(
		context.Background(),
		"abc1234",
	)
	if err != nil {
		t.Fatalf("GetURLByShortCode returned error: %v", err)
	}

	if found.ShortCode != "abc1234" {
		t.Errorf("expected short code %q, got %q", "abc1234", found.ShortCode)
	}

	if found.OriginalUrl != "https://example.com/cached" {
		t.Errorf(
			"expected original URL %q, got %q",
			"https://example.com/cached",
			found.OriginalUrl,
		)
	}

	if source.findCalled {
		t.Fatal("expected database not to be queried on cache hit")
	}

	if cache.setCalled {
		t.Fatal("expected cache not to be rewritten on cache hit")
	}
}

func TestCachedURLFinderCacheMissLoadsDatabaseAndPopulatesCache(t *testing.T) {
	cache := &fakeRedirectCache{
		getErr: urlcache.ErrMiss,
	}
	source := &fakeURLStore{
		found: database.Url{
			ShortCode:   "abc1234",
			OriginalUrl: "https://example.com/database",
		},
	}

	finder := newTestCachedURLFinder(cache, source)

	found, err := finder.GetURLByShortCode(
		context.Background(),
		"abc1234",
	)
	if err != nil {
		t.Fatalf("GetURLByShortCode returned error: %v", err)
	}

	if !source.findCalled {
		t.Fatal("expected database to be queried on cache miss")
	}

	if source.findCode != "abc1234" {
		t.Errorf(
			"expected database lookup for %q, got %q",
			"abc1234",
			source.findCode,
		)
	}

	if found.OriginalUrl != "https://example.com/database" {
		t.Errorf(
			"expected original URL %q, got %q",
			"https://example.com/database",
			found.OriginalUrl,
		)
	}

	if !cache.setCalled {
		t.Fatal("expected database result to populate cache")
	}

	if cache.setCode != "abc1234" {
		t.Errorf(
			"expected cached short code %q, got %q",
			"abc1234",
			cache.setCode,
		)
	}

	if cache.setURL != "https://example.com/database" {
		t.Errorf(
			"expected cached URL %q, got %q",
			"https://example.com/database",
			cache.setURL,
		)
	}
}

func TestCachedURLFinderCacheFailureFallsBackToDatabase(t *testing.T) {
	cache := &fakeRedirectCache{
		getErr: errors.New("redis unavailable"),
	}
	source := &fakeURLStore{
		found: database.Url{
			ShortCode:   "abc1234",
			OriginalUrl: "https://example.com/database",
		},
	}

	finder := newTestCachedURLFinder(cache, source)

	found, err := finder.GetURLByShortCode(
		context.Background(),
		"abc1234",
	)
	if err != nil {
		t.Fatalf("GetURLByShortCode returned error: %v", err)
	}

	if !source.findCalled {
		t.Fatal("expected database fallback when cache read fails")
	}

	if found.OriginalUrl != "https://example.com/database" {
		t.Errorf(
			"expected database URL %q, got %q",
			"https://example.com/database",
			found.OriginalUrl,
		)
	}
}

func TestCachedURLFinderCacheSetFailureDoesNotFailLookup(t *testing.T) {
	cache := &fakeRedirectCache{
		getErr: urlcache.ErrMiss,
		setErr: errors.New("redis unavailable"),
	}
	source := &fakeURLStore{
		found: database.Url{
			ShortCode:   "abc1234",
			OriginalUrl: "https://example.com/database",
		},
	}

	finder := newTestCachedURLFinder(cache, source)

	found, err := finder.GetURLByShortCode(
		context.Background(),
		"abc1234",
	)
	if err != nil {
		t.Fatalf("expected successful lookup despite cache write failure: %v", err)
	}

	if found.OriginalUrl != "https://example.com/database" {
		t.Errorf(
			"expected database URL %q, got %q",
			"https://example.com/database",
			found.OriginalUrl,
		)
	}

	if !cache.setCalled {
		t.Fatal("expected cache population attempt")
	}
}

func TestCachedURLFinderDatabaseNotFound(t *testing.T) {
	cache := &fakeRedirectCache{
		getErr: urlcache.ErrMiss,
	}
	source := &fakeURLStore{
		findErr: pgx.ErrNoRows,
	}

	finder := newTestCachedURLFinder(cache, source)

	_, err := finder.GetURLByShortCode(
		context.Background(),
		"missing",
	)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got %v", err)
	}

	if cache.setCalled {
		t.Fatal("expected missing URL not to be cached")
	}
}

func TestCachedURLFinderDatabaseFailure(t *testing.T) {
	cache := &fakeRedirectCache{
		getErr: urlcache.ErrMiss,
	}
	databaseErr := errors.New("database unavailable")
	source := &fakeURLStore{
		findErr: databaseErr,
	}

	finder := newTestCachedURLFinder(cache, source)

	_, err := finder.GetURLByShortCode(
		context.Background(),
		"abc1234",
	)
	if !errors.Is(err, databaseErr) {
		t.Fatalf("expected database error, got %v", err)
	}

	if cache.setCalled {
		t.Fatal("expected failed database lookup not to populate cache")
	}
}
