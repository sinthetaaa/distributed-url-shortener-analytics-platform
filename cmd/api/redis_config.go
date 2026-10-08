package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func redisOptionsFromEnv() (*redis.Options, error) {
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))

	if redisURL != "" {
		options, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, fmt.Errorf("parse REDIS_URL: %w", err)
		}

		applyRedisClientDefaults(options)

		return options, nil
	}

	redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisAddr == "" {
		return nil, fmt.Errorf("REDIS_URL or REDIS_ADDR is required")
	}

	options := &redis.Options{
		Addr: redisAddr,
	}

	applyRedisClientDefaults(options)

	return options, nil
}

func applyRedisClientDefaults(options *redis.Options) {
	options.MaxRetries = -1
	options.DialerRetries = 1
	options.DialerRetryTimeout = 10 * time.Millisecond
	options.DialTimeout = 50 * time.Millisecond
	options.ReadTimeout = 50 * time.Millisecond
	options.WriteTimeout = 50 * time.Millisecond
	options.ContextTimeoutEnabled = true
}
