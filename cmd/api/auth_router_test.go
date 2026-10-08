package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authpkg "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/auth"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

func newAuthIntegrationRouter(
	t *testing.T,
	service authenticationService,
	cookieSecure bool,
) http.Handler {
	t.Helper()

	logger := authTestLogger()
	store := &fakeURLStore{}

	return newRouterWithDependenciesMetricsAndAuth(
		logger,
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
		cookieSecure,
	)
}

func TestAuthRoutesAreWired(t *testing.T) {
	service := &fakeAuthenticationService{
		registerFn: func(
			_ context.Context,
			email string,
			password string,
		) (database.User, error) {
			return database.User{
				ID:    1,
				Email: email,
			}, nil
		},
		loginFn: func(
			_ context.Context,
			email string,
			password string,
		) (authpkg.Session, error) {
			return authpkg.Session{
				Token:     "session-token",
				ExpiresAt: time.Now().Add(time.Hour),
				User: database.User{
					ID:    1,
					Email: email,
				},
			}, nil
		},
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:    1,
				Email: "user@example.com",
			}, nil
		},
		logoutFn: func(
			context.Context,
			string,
		) error {
			return nil
		},
	}

	router := newAuthIntegrationRouter(
		t,
		service,
		false,
	)

	registerRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(
			`{"email":"user@example.com","password":"correct-password"}`,
		),
	)

	registerRecorder := httptest.NewRecorder()
	router.ServeHTTP(registerRecorder, registerRequest)

	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf(
			"register status = %d, want %d",
			registerRecorder.Code,
			http.StatusCreated,
		)
	}

	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(
			`{"email":"user@example.com","password":"correct-password"}`,
		),
	)

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)

	if loginRecorder.Code != http.StatusOK {
		t.Fatalf(
			"login status = %d, want %d",
			loginRecorder.Code,
			http.StatusOK,
		)
	}

	cookies := loginRecorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf(
			"login cookie count = %d, want 1",
			len(cookies),
		)
	}

	meRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/me",
		nil,
	)

	meRequest.AddCookie(cookies[0])

	meRecorder := httptest.NewRecorder()
	router.ServeHTTP(meRecorder, meRequest)

	if meRecorder.Code != http.StatusOK {
		t.Fatalf(
			"me status = %d, want %d",
			meRecorder.Code,
			http.StatusOK,
		)
	}

	var meResponse authUserResponse

	if err := json.NewDecoder(
		meRecorder.Body,
	).Decode(&meResponse); err != nil {
		t.Fatalf("decode me response: %v", err)
	}

	if meResponse.ID != 1 ||
		meResponse.Email != "user@example.com" {
		t.Fatalf(
			"me response = %+v",
			meResponse,
		)
	}

	logoutRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/logout",
		nil,
	)

	logoutRequest.AddCookie(cookies[0])

	logoutRecorder := httptest.NewRecorder()
	router.ServeHTTP(logoutRecorder, logoutRequest)

	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf(
			"logout status = %d, want %d",
			logoutRecorder.Code,
			http.StatusNoContent,
		)
	}
}

func TestMeRouteRequiresAuthentication(t *testing.T) {
	router := newAuthIntegrationRouter(
		t,
		&fakeAuthenticationService{},
		false,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/me",
		nil,
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusUnauthorized,
		)
	}
}

func TestExistingPublicRedirectRemainsPublicWithAuthEnabled(
	t *testing.T,
) {
	store := &fakeURLStore{
		found: database.Url{
			ShortCode:   "abc1234",
			OriginalUrl: "https://example.com",
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
		&fakeAuthenticationService{},
		false,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/abc1234",
		nil,
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusFound,
		)
	}
}
