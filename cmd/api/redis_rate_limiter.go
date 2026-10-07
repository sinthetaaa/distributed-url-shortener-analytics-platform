package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/redis/go-redis/v9"
)

const (
	createURLRateLimitRedisKeyPrefix = "shortscale:rate-limit:create-url:"
	rateLimitRedisOperationTimeout   = 50 * time.Millisecond

	rateLimitResultAllowed  = "allowed"
	rateLimitResultRejected = "rejected"
	rateLimitResultFailOpen = "fail_open"
)

const redisTokenBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_per_ms = tonumber(ARGV[2])
local ttl_ms = tonumber(ARGV[3])

local now = redis.call("TIME")
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)

local state = redis.call("HMGET", key, "tokens", "last_refill_ms")
local tokens = tonumber(state[1])
local last_refill_ms = tonumber(state[2])

if tokens == nil or last_refill_ms == nil then
	tokens = capacity
	last_refill_ms = now_ms
else
	local elapsed_ms = now_ms - last_refill_ms
	if elapsed_ms > 0 then
		tokens = math.min(
			capacity,
			tokens + elapsed_ms * refill_per_ms
		)
		last_refill_ms = now_ms
	end
end

local allowed = 0
local retry_after_ms = 0

if tokens >= 1 then
	tokens = tokens - 1
	allowed = 1
else
	retry_after_ms = math.ceil(
		(1 - tokens) / refill_per_ms
	)

	if retry_after_ms < 1 then
		retry_after_ms = 1
	end
end

redis.call(
	"HSET",
	key,
	"tokens",
	tostring(tokens),
	"last_refill_ms",
	tostring(last_refill_ms)
)
redis.call("PEXPIRE", key, ttl_ms)

return {allowed, retry_after_ms}
`

type redisTokenBucketLimiter struct {
	logger *slog.Logger
	client redis.Scripter
	script *redis.Script

	capacity        int
	refillPerSecond float64
	stateTTL        time.Duration
	metrics         *observability.Metrics
}

func newRedisTokenBucketLimiter(
	logger *slog.Logger,
	client redis.Scripter,
	capacity int,
	refillPerSecond float64,
) *redisTokenBucketLimiter {
	return newRedisTokenBucketLimiterWithMetrics(
		logger,
		client,
		capacity,
		refillPerSecond,
		nil,
	)
}

func newRedisTokenBucketLimiterWithMetrics(
	logger *slog.Logger,
	client redis.Scripter,
	capacity int,
	refillPerSecond float64,
	metrics *observability.Metrics,
) *redisTokenBucketLimiter {
	if capacity <= 0 {
		panic("rate-limit capacity must be positive")
	}

	if refillPerSecond <= 0 {
		panic("rate-limit refill rate must be positive")
	}

	fullRefillSeconds := math.Ceil(
		float64(capacity) / refillPerSecond,
	)
	stateTTL := time.Duration(
		fullRefillSeconds*2,
	) * time.Second

	if stateTTL < time.Minute {
		stateTTL = time.Minute
	}

	return &redisTokenBucketLimiter{
		logger:          logger,
		client:          client,
		script:          redis.NewScript(redisTokenBucketScript),
		capacity:        capacity,
		refillPerSecond: refillPerSecond,
		stateTTL:        stateTTL,
		metrics:         metrics,
	}
}

func (l *redisTokenBucketLimiter) Allow(identity string) rateLimitDecision {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		rateLimitRedisOperationTimeout,
	)
	defer cancel()

	result, err := l.script.Run(
		ctx,
		l.client,
		[]string{
			createURLRateLimitRedisKeyPrefix + identity,
		},
		l.capacity,
		l.refillPerSecond/1000,
		l.stateTTL.Milliseconds(),
	).Slice()
	if err != nil {
		l.logger.Warn(
			"distributed rate limiter unavailable; allowing request",
			"identity", identity,
			"error", err,
		)

		l.recordDecision(rateLimitResultFailOpen)

		return rateLimitDecision{
			Allowed: true,
		}
	}

	if len(result) != 2 {
		l.logger.Warn(
			"distributed rate limiter returned invalid result; allowing request",
			"identity", identity,
			"result_length", len(result),
		)

		l.recordDecision(rateLimitResultFailOpen)

		return rateLimitDecision{
			Allowed: true,
		}
	}

	allowed, err := redisResultInt64(result[0])
	if err != nil {
		l.logger.Warn(
			"distributed rate limiter returned invalid allowed value; allowing request",
			"identity", identity,
			"error", err,
		)

		l.recordDecision(rateLimitResultFailOpen)

		return rateLimitDecision{
			Allowed: true,
		}
	}

	retryAfterMilliseconds, err := redisResultInt64(result[1])
	if err != nil {
		l.logger.Warn(
			"distributed rate limiter returned invalid retry value; allowing request",
			"identity", identity,
			"error", err,
		)

		l.recordDecision(rateLimitResultFailOpen)

		return rateLimitDecision{
			Allowed: true,
		}
	}

	if allowed == 1 {
		l.recordDecision(rateLimitResultAllowed)

		return rateLimitDecision{
			Allowed: true,
		}
	}

	if retryAfterMilliseconds < 1 {
		retryAfterMilliseconds = 1
	}

	l.recordDecision(rateLimitResultRejected)

	return rateLimitDecision{
		Allowed: false,
		RetryAfter: time.Duration(
			retryAfterMilliseconds,
		) * time.Millisecond,
	}
}

func (l *redisTokenBucketLimiter) recordDecision(result string) {
	if l.metrics == nil {
		return
	}

	l.metrics.RateLimitDecisionsTotal.
		WithLabelValues(result).
		Inc()
}

func redisResultInt64(value any) (int64, error) {
	switch value := value.(type) {
	case int64:
		return value, nil

	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf(
				"parse Redis integer %q: %w",
				value,
				err,
			)
		}

		return parsed, nil
	case []byte:
		parsed, err := strconv.ParseInt(
			string(value),
			10,
			64,
		)
		if err != nil {
			return 0, fmt.Errorf(
				"parse Redis integer %q: %w",
				string(value),
				err,
			)
		}

		return parsed, nil

	default:
		return 0, fmt.Errorf(
			"unexpected Redis result type %T",
			value,
		)
	}
}
