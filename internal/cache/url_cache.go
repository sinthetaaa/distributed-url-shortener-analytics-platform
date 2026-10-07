package cache

import (
	"context"
	"errors"
	"hash/fnv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	urlKeyPrefix     = "shortscale:url:"
	ttlJitterPercent = 10
)

var ErrMiss = errors.New("cache miss")

type redisStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
}

type redisClientStore struct {
	client *redis.Client
}

func (s redisClientStore) Get(ctx context.Context, key string) (string, error) {
	return s.client.Get(ctx, key).Result()
}

func (s redisClientStore) Set(
	ctx context.Context,
	key string,
	value string,
	ttl time.Duration,
) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

type ttlPolicy func(time.Duration, string) time.Duration

type URLCache struct {
	store     redisStore
	ttl       time.Duration
	ttlForKey ttlPolicy
}

func NewURLCache(client *redis.Client, ttl time.Duration) *URLCache {
	return &URLCache{
		store: redisClientStore{
			client: client,
		},
		ttl:       ttl,
		ttlForKey: jitteredTTL,
	}
}

func (c *URLCache) Get(ctx context.Context, shortCode string) (string, error) {
	value, err := c.store.Get(ctx, cacheKey(shortCode))
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrMiss
		}

		return "", err
	}

	return value, nil
}

func (c *URLCache) Set(
	ctx context.Context,
	shortCode string,
	originalURL string,
) error {
	ttl := c.ttl

	if c.ttlForKey != nil {
		ttl = c.ttlForKey(c.ttl, shortCode)
	}

	return c.store.Set(
		ctx,
		cacheKey(shortCode),
		originalURL,
		ttl,
	)
}

func jitteredTTL(
	baseTTL time.Duration,
	shortCode string,
) time.Duration {
	if baseTTL <= 0 {
		return baseTTL
	}

	maxJitter := baseTTL * ttlJitterPercent / 100
	if maxJitter <= 0 {
		return baseTTL
	}

	hasher := fnv.New64a()

	_, _ = hasher.Write([]byte(shortCode))

	hash := hasher.Sum64()
	jitterSpan := uint64(maxJitter)*2 + 1
	offset := time.Duration(hash%jitterSpan) - maxJitter

	return baseTTL + offset
}

func cacheKey(shortCode string) string {
	return urlKeyPrefix + shortCode
}
