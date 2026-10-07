package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const urlKeyPrefix = "shortscale:url:"

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

type URLCache struct {
	store redisStore
	ttl   time.Duration
}

func NewURLCache(client *redis.Client, ttl time.Duration) *URLCache {
	return &URLCache{
		store: redisClientStore{
			client: client,
		},
		ttl: ttl,
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
	return c.store.Set(
		ctx,
		cacheKey(shortCode),
		originalURL,
		c.ttl,
	)
}

func cacheKey(shortCode string) string {
	return urlKeyPrefix + shortCode
}
