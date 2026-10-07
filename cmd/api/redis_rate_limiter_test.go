package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisResultInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{
			name:  "int64",
			value: int64(7),
			want:  7,
		},
		{
			name:  "string",
			value: "8",
			want:  8,
		},
		{
			name:  "bytes",
			value: []byte("9"),
			want:  9,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := redisResultInt64(test.value)
			if err != nil {
				t.Fatalf(
					"redisResultInt64 returned error: %v",
					err,
				)
			}

			if got != test.want {
				t.Fatalf(
					"expected %d, got %d",
					test.want,
					got,
				)
			}
		})
	}
}

func TestRedisTokenBucketStateTTLAllowsFullRefill(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	limiter := newRedisTokenBucketLimiter(
		logger,
		nil,
		createURLRateLimitCapacity,
		createURLRateLimitRefillPerSecond,
	)

	fullRefillDuration := time.Duration(
		mathCeilSeconds(
			float64(createURLRateLimitCapacity)/
				createURLRateLimitRefillPerSecond,
		),
	) * time.Second

	if limiter.stateTTL < fullRefillDuration {
		t.Fatalf(
			"state TTL %s is shorter than full refill duration %s",
			limiter.stateTTL,
			fullRefillDuration,
		)
	}
}

func TestRedisTokenBucketSharedAcrossInstances(t *testing.T) {
	client := redisIntegrationClient(t)

	identity := fmt.Sprintf(
		"198.51.100.%d",
		time.Now().UnixNano()%200+1,
	)
	key := createURLRateLimitRedisKeyPrefix + identity

	t.Cleanup(func() {
		_ = client.Del(
			context.Background(),
			key,
		).Err()
	})

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	first := newRedisTokenBucketLimiter(
		logger,
		client,
		createURLRateLimitCapacity,
		0.000001,
	)
	second := newRedisTokenBucketLimiter(
		logger,
		client,
		createURLRateLimitCapacity,
		0.000001,
	)

	for i := 0; i < createURLRateLimitCapacity; i++ {
		limiter := first
		if i%2 == 1 {
			limiter = second
		}

		if !limiter.Allow(identity).Allowed {
			t.Fatalf(
				"expected shared request %d to be allowed",
				i+1,
			)
		}
	}

	if second.Allow(identity).Allowed {
		t.Fatal(
			"expected second limiter instance to observe exhausted shared bucket",
		)
	}
}

func TestRedisTokenBucketLuaIsAtomicUnderConcurrency(t *testing.T) {
	client := redisIntegrationClient(t)

	identity := fmt.Sprintf(
		"203.0.113.%d",
		time.Now().UnixNano()%200+1,
	)
	key := createURLRateLimitRedisKeyPrefix + identity

	t.Cleanup(func() {
		_ = client.Del(
			context.Background(),
			key,
		).Err()
	})

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	limiters := []*redisTokenBucketLimiter{
		newRedisTokenBucketLimiter(
			logger,
			client,
			createURLRateLimitCapacity,
			0.000001,
		),
		newRedisTokenBucketLimiter(
			logger,
			client,
			createURLRateLimitCapacity,
			0.000001,
		),
		newRedisTokenBucketLimiter(
			logger,
			client,
			createURLRateLimitCapacity,
			0.000001,
		),
	}

	const concurrentRequests = 60

	start := make(chan struct{})

	var ready sync.WaitGroup
	ready.Add(concurrentRequests)

	var requests sync.WaitGroup
	requests.Add(concurrentRequests)

	var allowed atomic.Int64

	for i := 0; i < concurrentRequests; i++ {
		limiter := limiters[i%len(limiters)]

		go func() {
			defer requests.Done()

			ready.Done()
			<-start

			if limiter.Allow(identity).Allowed {
				allowed.Add(1)
			}
		}()
	}

	ready.Wait()
	close(start)

	requests.Wait()

	if got := allowed.Load(); got != createURLRateLimitCapacity {
		t.Fatalf(
			"expected exactly %d requests to consume the shared burst, got %d",
			createURLRateLimitCapacity,
			got,
		)
	}
}

func redisIntegrationClient(t *testing.T) *redis.Client {
	t.Helper()

	addr := os.Getenv(
		"SHORTSCALE_REDIS_INTEGRATION_ADDR",
	)
	if addr == "" {
		t.Skip(
			"SHORTSCALE_REDIS_INTEGRATION_ADDR is not set",
		)
	}

	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	t.Cleanup(func() {
		_ = client.Close()
	})

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf(
			"ping Redis integration service: %v",
			err,
		)
	}

	return client
}

func mathCeilSeconds(value float64) int64 {
	whole := int64(value)
	if float64(whole) == value {
		return whole
	}

	return whole + 1
}
