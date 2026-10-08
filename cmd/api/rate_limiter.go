package main

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	createURLRateLimitCapacity        = 5
	createURLRateLimitRefillPerSecond = 10.0 / 60.0

	authRateLimitCapacity        = 5
	authRateLimitRefillPerSecond = 5.0 / 60.0
)

type rateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type requestRateLimiter interface {
	Allow(string) rateLimitDecision
}

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

type localTokenBucketLimiter struct {
	mu sync.Mutex

	buckets map[string]tokenBucket

	capacity        float64
	refillPerSecond float64

	now func() time.Time
}

func newLocalTokenBucketLimiter(
	capacity int,
	refillPerSecond float64,
) *localTokenBucketLimiter {
	if capacity <= 0 {
		panic("rate-limit capacity must be positive")
	}

	if refillPerSecond <= 0 {
		panic("rate-limit refill rate must be positive")
	}

	return &localTokenBucketLimiter{
		buckets:         make(map[string]tokenBucket),
		capacity:        float64(capacity),
		refillPerSecond: refillPerSecond,
		now:             time.Now,
	}
}

func (l *localTokenBucketLimiter) Allow(identity string) rateLimitDecision {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[identity]
	if !ok {
		bucket = tokenBucket{
			tokens:     l.capacity,
			lastRefill: now,
		}
	} else {
		elapsed := now.Sub(bucket.lastRefill).Seconds()
		if elapsed > 0 {
			bucket.tokens = math.Min(
				l.capacity,
				bucket.tokens+elapsed*l.refillPerSecond,
			)
			bucket.lastRefill = now
		}
	}

	if bucket.tokens >= 1 {
		bucket.tokens--
		l.buckets[identity] = bucket

		return rateLimitDecision{
			Allowed: true,
		}
	}

	l.buckets[identity] = bucket

	missingTokens := 1 - bucket.tokens
	retrySeconds := math.Ceil(missingTokens / l.refillPerSecond)

	if retrySeconds < 1 {
		retrySeconds = 1
	}

	return rateLimitDecision{
		Allowed:    false,
		RetryAfter: time.Duration(retrySeconds) * time.Second,
	}
}

func rateLimitMiddleware(
	logger *slog.Logger,
	limiter requestRateLimiter,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decision := limiter.Allow(clientIP(r))
			if decision.Allowed {
				next.ServeHTTP(w, r)
				return
			}

			retryAfter := int64(math.Ceil(decision.RetryAfter.Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}

			w.Header().Set(
				"Retry-After",
				strconv.FormatInt(retryAfter, 10),
			)

			writeJSON(
				w,
				logger,
				http.StatusTooManyRequests,
				map[string]string{
					"error": "rate limit exceeded",
				},
			)
		})
	}
}

func clientIP(r *http.Request) string {
	// ShortScale's API containers are not exposed publicly; Nginx is the
	// trusted ingress and overwrites X-Real-IP with its observed client.
	if ip := canonicalIP(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}

	// Fall back to the right-most X-Forwarded-For entry. With one trusted
	// proxy this is the address appended by that proxy, rather than a
	// potentially spoofed left-most value supplied by a client.
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")

		for i := len(parts) - 1; i >= 0; i-- {
			if ip := canonicalIP(parts[i]); ip != "" {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		if ip := canonicalIP(host); ip != "" {
			return ip
		}
	}

	if ip := canonicalIP(r.RemoteAddr); ip != "" {
		return ip
	}

	return "unknown"
}

func canonicalIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}

	return ip.String()
}
