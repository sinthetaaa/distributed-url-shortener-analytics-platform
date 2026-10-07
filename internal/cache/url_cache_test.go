package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeRedisStore struct {
	getValue string
	getErr   error
	getKey   string

	setKey   string
	setValue string
	setTTL   time.Duration
	setErr   error
}

func (f *fakeRedisStore) Get(_ context.Context, key string) (string, error) {
	f.getKey = key
	return f.getValue, f.getErr
}

func (f *fakeRedisStore) Set(
	_ context.Context,
	key string,
	value string,
	ttl time.Duration,
) error {
	f.setKey = key
	f.setValue = value
	f.setTTL = ttl
	return f.setErr
}

func TestURLCacheGetHit(t *testing.T) {
	store := &fakeRedisStore{
		getValue: "https://example.com/article",
	}

	cache := &URLCache{
		store: store,
		ttl:   time.Hour,
	}

	value, err := cache.Get(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if value != "https://example.com/article" {
		t.Errorf(
			"expected cached URL %q, got %q",
			"https://example.com/article",
			value,
		)
	}

	if store.getKey != "shortscale:url:abc1234" {
		t.Errorf(
			"expected Redis key %q, got %q",
			"shortscale:url:abc1234",
			store.getKey,
		)
	}
}

func TestURLCacheGetMiss(t *testing.T) {
	store := &fakeRedisStore{
		getErr: redis.Nil,
	}

	cache := &URLCache{
		store: store,
		ttl:   time.Hour,
	}

	_, err := cache.Get(context.Background(), "missing")
	if !errors.Is(err, ErrMiss) {
		t.Fatalf("expected ErrMiss, got %v", err)
	}
}

func TestURLCacheGetFailure(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	store := &fakeRedisStore{
		getErr: redisErr,
	}

	cache := &URLCache{
		store: store,
		ttl:   time.Hour,
	}

	_, err := cache.Get(context.Background(), "abc1234")
	if !errors.Is(err, redisErr) {
		t.Fatalf("expected Redis error, got %v", err)
	}

	if errors.Is(err, ErrMiss) {
		t.Fatal("expected Redis failure not to be treated as cache miss")
	}
}

func TestURLCacheSet(t *testing.T) {
	store := &fakeRedisStore{}

	cache := &URLCache{
		store: store,
		ttl:   30 * time.Minute,
	}

	err := cache.Set(
		context.Background(),
		"abc1234",
		"https://example.com/article",
	)
	if err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if store.setKey != "shortscale:url:abc1234" {
		t.Errorf(
			"expected Redis key %q, got %q",
			"shortscale:url:abc1234",
			store.setKey,
		)
	}

	if store.setValue != "https://example.com/article" {
		t.Errorf(
			"expected cached URL %q, got %q",
			"https://example.com/article",
			store.setValue,
		)
	}

	if store.setTTL != 30*time.Minute {
		t.Errorf(
			"expected TTL %s, got %s",
			30*time.Minute,
			store.setTTL,
		)
	}
}

func TestURLCacheSetFailure(t *testing.T) {
	redisErr := errors.New("redis unavailable")

	store := &fakeRedisStore{
		setErr: redisErr,
	}

	cache := &URLCache{
		store: store,
		ttl:   time.Hour,
	}

	err := cache.Set(
		context.Background(),
		"abc1234",
		"https://example.com/article",
	)
	if !errors.Is(err, redisErr) {
		t.Fatalf("expected Redis error, got %v", err)
	}
}

func TestCacheKey(t *testing.T) {
	key := cacheKey("3ZB9CeC")

	if key != "shortscale:url:3ZB9CeC" {
		t.Errorf(
			"expected key %q, got %q",
			"shortscale:url:3ZB9CeC",
			key,
		)
	}
}
