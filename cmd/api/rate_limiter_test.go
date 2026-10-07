package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubRateLimiter struct {
	decision rateLimitDecision
	identity string
}

func (s *stubRateLimiter) Allow(identity string) rateLimitDecision {
	s.identity = identity
	return s.decision
}

func TestLocalTokenBucketAllowsBurstThenLimits(t *testing.T) {
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)

	limiter := newLocalTokenBucketLimiter(
		createURLRateLimitCapacity,
		createURLRateLimitRefillPerSecond,
	)
	limiter.now = func() time.Time {
		return now
	}

	for i := 0; i < createURLRateLimitCapacity; i++ {
		decision := limiter.Allow("203.0.113.10")
		if !decision.Allowed {
			t.Fatalf("expected request %d in burst to be allowed", i+1)
		}
	}

	decision := limiter.Allow("203.0.113.10")
	if decision.Allowed {
		t.Fatal("expected request after burst capacity to be rejected")
	}

	if decision.RetryAfter != 6*time.Second {
		t.Fatalf(
			"expected retry-after %s, got %s",
			6*time.Second,
			decision.RetryAfter,
		)
	}
}

func TestLocalTokenBucketRefillsGradually(t *testing.T) {
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)

	limiter := newLocalTokenBucketLimiter(
		createURLRateLimitCapacity,
		createURLRateLimitRefillPerSecond,
	)
	limiter.now = func() time.Time {
		return now
	}

	for i := 0; i < createURLRateLimitCapacity; i++ {
		if !limiter.Allow("203.0.113.10").Allowed {
			t.Fatalf("expected burst request %d to be allowed", i+1)
		}
	}

	if limiter.Allow("203.0.113.10").Allowed {
		t.Fatal("expected exhausted bucket to reject request")
	}

	now = now.Add(6 * time.Second)

	if !limiter.Allow("203.0.113.10").Allowed {
		t.Fatal("expected one token to refill after six seconds")
	}

	if limiter.Allow("203.0.113.10").Allowed {
		t.Fatal("expected only one token to have refilled")
	}
}

func TestLocalTokenBucketSeparatesClientIdentities(t *testing.T) {
	limiter := newLocalTokenBucketLimiter(1, 1)

	if !limiter.Allow("203.0.113.10").Allowed {
		t.Fatal("expected first client to be allowed")
	}

	if limiter.Allow("203.0.113.10").Allowed {
		t.Fatal("expected first client's bucket to be exhausted")
	}

	if !limiter.Allow("198.51.100.20").Allowed {
		t.Fatal("expected second client to have an independent bucket")
	}
}

func TestRateLimitMiddlewareRejectsWith429AndRetryAfter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	limiter := &stubRateLimiter{
		decision: rateLimitDecision{
			Allowed:    false,
			RetryAfter: 4 * time.Second,
		},
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	handler := rateLimitMiddleware(logger, limiter)(next)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		nil,
	)
	request.Header.Set("X-Real-IP", "203.0.113.10")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusTooManyRequests,
			recorder.Code,
		)
	}

	if nextCalled {
		t.Fatal("expected rejected request not to reach next handler")
	}

	if got := recorder.Header().Get("Retry-After"); got != "4" {
		t.Fatalf("expected Retry-After %q, got %q", "4", got)
	}

	expectedBody := `{"error":"rate limit exceeded"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Fatalf(
			"expected body %q, got %q",
			expectedBody,
			actualBody,
		)
	}

	if limiter.identity != "203.0.113.10" {
		t.Fatalf(
			"expected limiter identity %q, got %q",
			"203.0.113.10",
			limiter.identity,
		)
	}
}

func TestRateLimitMiddlewareAllowsRequest(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	limiter := &stubRateLimiter{
		decision: rateLimitDecision{
			Allowed: true,
		},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := rateLimitMiddleware(logger, limiter)(next)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		nil,
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusNoContent,
			recorder.Code,
		)
	}
}

func TestClientIPPrefersRealIP(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("X-Real-IP", "203.0.113.10")
	request.Header.Set(
		"X-Forwarded-For",
		"198.51.100.1, 198.51.100.2",
	)

	if got := clientIP(request); got != "203.0.113.10" {
		t.Fatalf(
			"expected X-Real-IP %q, got %q",
			"203.0.113.10",
			got,
		)
	}
}

func TestClientIPUsesRightmostForwardedAddress(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set(
		"X-Forwarded-For",
		"198.51.100.1, 203.0.113.10",
	)

	if got := clientIP(request); got != "203.0.113.10" {
		t.Fatalf(
			"expected right-most forwarded IP %q, got %q",
			"203.0.113.10",
			got,
		)
	}
}

func TestClientIPFallsBackToRemoteAddress(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.25:54321"

	if got := clientIP(request); got != "192.0.2.25" {
		t.Fatalf(
			"expected remote IP %q, got %q",
			"192.0.2.25",
			got,
		)
	}
}

func TestCreateURLRouteRateLimitsInitialBurst(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	for i := 0; i < createURLRateLimitCapacity; i++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/urls",
			strings.NewReader(`{"url":"https://example.com/article"}`),
		)
		request.RemoteAddr = "203.0.113.10:50000"

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusCreated {
			t.Fatalf(
				"expected burst request %d to return %d, got %d: %s",
				i+1,
				http.StatusCreated,
				recorder.Code,
				recorder.Body.String(),
			)
		}
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"https://example.com/article"}`),
	)
	request.RemoteAddr = "203.0.113.10:50001"

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"expected request after burst to return %d, got %d: %s",
			http.StatusTooManyRequests,
			recorder.Code,
			recorder.Body.String(),
		)
	}

	if creator.attempts != createURLRateLimitCapacity {
		t.Fatalf(
			"expected only %d requests to reach URL creation, got %d",
			createURLRateLimitCapacity,
			creator.attempts,
		)
	}
}
