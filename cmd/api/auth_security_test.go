package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authpkg "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/auth"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

func TestRegisterRejectsTrailingJSON(t *testing.T) {
	called := false

	service := &fakeAuthenticationService{
		registerFn: func(
			context.Context,
			string,
			string,
		) (database.User, error) {
			called = true
			return database.User{}, nil
		},
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(
			`{"email":"user@example.com","password":"correct-password"} {"extra":true}`,
		),
	)

	recorder := httptest.NewRecorder()

	registerHandler(
		authTestLogger(),
		service,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}

	if called {
		t.Fatal(
			"registration service must not run for trailing JSON",
		)
	}
}

func TestLoginRejectsOversizedBody(t *testing.T) {
	called := false

	service := &fakeAuthenticationService{
		loginFn: func(
			context.Context,
			string,
			string,
		) (authpkg.Session, error) {
			called = true
			return authpkg.Session{}, nil
		},
	}

	oversizedPassword := strings.Repeat(
		"x",
		maxAuthRequestBodyBytes,
	)

	body := `{"email":"user@example.com","password":"` +
		oversizedPassword +
		`"}`

	if len(body) <= maxAuthRequestBodyBytes {
		t.Fatalf(
			"test body size = %d, must exceed %d",
			len(body),
			maxAuthRequestBodyBytes,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(body),
	)

	recorder := httptest.NewRecorder()

	loginHandler(
		authTestLogger(),
		service,
		false,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}

	if called {
		t.Fatal(
			"login service must not run for oversized body",
		)
	}
}

func TestAuthRoutesAreRateLimited(t *testing.T) {
	store := &fakeURLStore{}

	service := &fakeAuthenticationService{
		loginFn: func(
			context.Context,
			string,
			string,
		) (authpkg.Session, error) {
			return authpkg.Session{},
				authpkg.ErrInvalidCredentials
		},
	}

	router := newRouterWithDependenciesMetricsAndAuth(
		authTestLogger(),
		fakeDatabasePinger{},
		store,
		store,
		newLocalTokenBucketLimiter(
			createURLRateLimitCapacity,
			createURLRateLimitRefillPerSecond,
		),
		noopRedirectEventRecorder{},
		nil,
		nil,
		service,
		false,
	)

	for attempt := 1; attempt <= authRateLimitCapacity; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/auth/login",
			strings.NewReader(
				`{"email":"user@example.com","password":"wrong-password"}`,
			),
		)

		request.RemoteAddr = "203.0.113.10:5000"

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf(
				"attempt %d status = %d, want %d",
				attempt,
				recorder.Code,
				http.StatusUnauthorized,
			)
		}
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(
			`{"email":"user@example.com","password":"wrong-password"}`,
		),
	)

	request.RemoteAddr = "203.0.113.10:5000"

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"rate-limited status = %d, want %d",
			recorder.Code,
			http.StatusTooManyRequests,
		)
	}

	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal(
			"rate-limited response must include Retry-After",
		)
	}
}

func TestAuthRateLimitSeparatesClientIPs(t *testing.T) {
	store := &fakeURLStore{}

	service := &fakeAuthenticationService{
		loginFn: func(
			context.Context,
			string,
			string,
		) (authpkg.Session, error) {
			return authpkg.Session{},
				authpkg.ErrInvalidCredentials
		},
	}

	router := newRouterWithDependenciesMetricsAndAuth(
		authTestLogger(),
		fakeDatabasePinger{},
		store,
		store,
		newLocalTokenBucketLimiter(
			createURLRateLimitCapacity,
			createURLRateLimitRefillPerSecond,
		),
		noopRedirectEventRecorder{},
		nil,
		nil,
		service,
		false,
	)

	for attempt := 0; attempt < authRateLimitCapacity; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/auth/login",
			strings.NewReader(
				`{"email":"user@example.com","password":"wrong-password"}`,
			),
		)
		request.RemoteAddr = "203.0.113.20:5000"

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(
			`{"email":"user@example.com","password":"wrong-password"}`,
		),
	)
	request.RemoteAddr = "203.0.113.21:5000"

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"second-client status = %d, want %d",
			recorder.Code,
			http.StatusUnauthorized,
		)
	}
}
