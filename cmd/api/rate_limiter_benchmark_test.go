package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	rateLimiterBenchmarkCapacity        = 1_000_000_000
	rateLimiterBenchmarkRefillPerSecond = 1_000_000_000
	rateLimiterBenchmarkIdentity        = "203.0.113.250"
)

type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func newBenchmarkResponseWriter() *benchmarkResponseWriter {
	return &benchmarkResponseWriter{
		header: make(http.Header),
	}
}

func (w *benchmarkResponseWriter) Header() http.Header {
	return w.header
}

func (w *benchmarkResponseWriter) Write(body []byte) (int, error) {
	return len(body), nil
}

func (w *benchmarkResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *benchmarkResponseWriter) reset() {
	w.status = 0

	for key := range w.header {
		delete(w.header, key)
	}
}

func BenchmarkRateLimitMiddlewareBaseline(b *testing.B) {
	handler := benchmarkTerminalHandler()
	request := benchmarkRateLimitRequest()
	writer := newBenchmarkResponseWriter()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writer.reset()
		handler.ServeHTTP(writer, request)

		if writer.status != http.StatusNoContent {
			b.Fatalf(
				"expected status %d, got %d",
				http.StatusNoContent,
				writer.status,
			)
		}
	}
}

func BenchmarkRateLimitMiddlewareLocal(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := newLocalTokenBucketLimiter(
		rateLimiterBenchmarkCapacity,
		rateLimiterBenchmarkRefillPerSecond,
	)
	handler := rateLimitMiddleware(logger, limiter)(
		benchmarkTerminalHandler(),
	)
	request := benchmarkRateLimitRequest()
	writer := newBenchmarkResponseWriter()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writer.reset()
		handler.ServeHTTP(writer, request)

		if writer.status != http.StatusNoContent {
			b.Fatalf(
				"expected status %d, got %d",
				http.StatusNoContent,
				writer.status,
			)
		}
	}
}

func BenchmarkRateLimitMiddlewareRedis(b *testing.B) {
	addr := os.Getenv("SHORTSCALE_REDIS_INTEGRATION_ADDR")
	if addr == "" {
		b.Skip("SHORTSCALE_REDIS_INTEGRATION_ADDR is not set")
	}

	client := redis.NewClient(&redis.Options{
		Addr:                  addr,
		MaxRetries:            -1,
		DialerRetries:         1,
		DialerRetryTimeout:    10 * time.Millisecond,
		DialTimeout:           50 * time.Millisecond,
		ReadTimeout:           50 * time.Millisecond,
		WriteTimeout:          50 * time.Millisecond,
		ContextTimeoutEnabled: true,
	})
	b.Cleanup(func() {
		_ = client.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		b.Fatalf("ping Redis benchmark service: %v", err)
	}

	key := createURLRateLimitRedisKeyPrefix + rateLimiterBenchmarkIdentity

	if err := client.Del(context.Background(), key).Err(); err != nil {
		b.Fatalf("clear Redis benchmark key: %v", err)
	}
	b.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := newRedisTokenBucketLimiter(
		logger,
		client,
		rateLimiterBenchmarkCapacity,
		rateLimiterBenchmarkRefillPerSecond,
	)
	handler := rateLimitMiddleware(logger, limiter)(
		benchmarkTerminalHandler(),
	)
	request := benchmarkRateLimitRequest()
	writer := newBenchmarkResponseWriter()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writer.reset()
		handler.ServeHTTP(writer, request)

		if writer.status != http.StatusNoContent {
			b.Fatalf(
				"expected status %d, got %d",
				http.StatusNoContent,
				writer.status,
			)
		}
	}
}

func benchmarkTerminalHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func benchmarkRateLimitRequest() *http.Request {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		nil,
	)
	request.Header.Set(
		"X-Real-IP",
		rateLimiterBenchmarkIdentity,
	)

	return request
}
