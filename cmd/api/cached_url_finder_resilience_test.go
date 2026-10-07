package main

import (
	"context"
	"errors"
	"testing"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

func TestCachedURLFinderCacheReadFailureSkipsCacheWrite(t *testing.T) {
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

	if found.OriginalUrl != "https://example.com/database" {
		t.Errorf(
			"expected database URL %q, got %q",
			"https://example.com/database",
			found.OriginalUrl,
		)
	}

	if !source.findCalled {
		t.Fatal("expected database fallback after cache read failure")
	}

	if cache.setCalled {
		t.Fatal("expected cache write to be skipped after cache read failure")
	}
}

type deadlineRecordingCache struct {
	setHadDeadline bool
}

func (c *deadlineRecordingCache) Get(
	_ context.Context,
	_ string,
) (string, error) {
	return "", urlcache.ErrMiss
}

func (c *deadlineRecordingCache) Set(
	ctx context.Context,
	_ string,
	_ string,
) error {
	_, c.setHadDeadline = ctx.Deadline()
	return nil
}

func TestCachedURLFinderCacheSetUsesBoundedContext(t *testing.T) {
	cache := &deadlineRecordingCache{}

	source := &fakeURLStore{
		found: database.Url{
			ShortCode:   "abc1234",
			OriginalUrl: "https://example.com/database",
		},
	}

	finder := newTestCachedURLFinder(cache, source)

	_, err := finder.GetURLByShortCode(
		context.Background(),
		"abc1234",
	)
	if err != nil {
		t.Fatalf("GetURLByShortCode returned error: %v", err)
	}

	if !cache.setHadDeadline {
		t.Fatal("expected cache write context to have a deadline")
	}
}
