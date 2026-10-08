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

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestCreateURLRequiresAuthenticationWhenAuthEnabled(
	t *testing.T,
) {
	store := &fakeURLStore{}

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
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(
			`{"url":"https://example.com/article"}`,
		),
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

	if store.createCalled {
		t.Fatal("legacy CreateURL must not be called")
	}

	if store.createOwnedCalled {
		t.Fatal("CreateOwnedURL must not run without authentication")
	}
}

func TestCreateURLAssignsAuthenticatedOwner(t *testing.T) {
	store := &fakeURLStore{}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			_ context.Context,
			token string,
		) (database.User, error) {
			if token != "valid-session-token" {
				t.Fatalf(
					"Authenticate token = %q",
					token,
				)
			}

			return database.User{
				ID:    81,
				Email: "owner@example.com",
			}, nil
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

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(
			`{"url":"https://example.com/article"}`,
		),
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "valid-session-token",
		},
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf(
			"status = %d, want %d: %s",
			recorder.Code,
			http.StatusCreated,
			recorder.Body.String(),
		)
	}

	if !store.createOwnedCalled {
		t.Fatal("expected CreateOwnedURL to be called")
	}

	if store.createCalled {
		t.Fatal("legacy CreateURL must not be called")
	}

	if !store.createOwnedParams.UserID.Valid {
		t.Fatal("created URL owner must be non-null")
	}

	if store.createOwnedParams.UserID.Int64 != 81 {
		t.Fatalf(
			"owner ID = %d, want 81",
			store.createOwnedParams.UserID.Int64,
		)
	}

	if store.createOwnedParams.OriginalUrl !=
		"https://example.com/article" {
		t.Fatalf(
			"original URL = %q",
			store.createOwnedParams.OriginalUrl,
		)
	}
}

func TestOwnedURLCreationPreservesCollisionRetry(
	t *testing.T,
) {
	collision := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "urls_short_code_key",
	}

	store := &fakeURLStore{
		createErrors: []error{
			collision,
			nil,
		},
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:    91,
				Email: "owner@example.com",
			}, nil
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

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(
			`{"url":"https://example.com"}`,
		),
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "valid-session-token",
		},
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf(
			"status = %d, want %d: %s",
			recorder.Code,
			http.StatusCreated,
			recorder.Body.String(),
		)
	}

	if store.attempts != 2 {
		t.Fatalf(
			"creation attempts = %d, want 2",
			store.attempts,
		)
	}

	if store.createOwnedParams.UserID.Int64 != 91 {
		t.Fatalf(
			"owner ID = %d, want 91",
			store.createOwnedParams.UserID.Int64,
		)
	}
}

func TestListUserURLsRequiresAuthentication(t *testing.T) {
	store := &fakeURLStore{}

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
		"/api/v1/urls",
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

	if store.listCalled {
		t.Fatal("ListURLsByUser must not run without authentication")
	}
}

func TestListUserURLsUsesAuthenticatedOwner(t *testing.T) {
	createdAt := time.Date(
		2026,
		time.October,
		8,
		12,
		30,
		0,
		0,
		time.UTC,
	)

	store := &fakeURLStore{
		listed: []database.Url{
			{
				ID:          15,
				ShortCode:   "recent1",
				OriginalUrl: "https://example.com/recent",
				CreatedAt: pgtype.Timestamptz{
					Time:  createdAt,
					Valid: true,
				},
				ExpiresAt: pgtype.Timestamptz{
					Valid: false,
				},
				UserID: pgtype.Int8{
					Int64: 44,
					Valid: true,
				},
			},
		},
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:    44,
				Email: "owner@example.com",
			}, nil
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

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls?limit=7",
		nil,
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "valid-session-token",
		},
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d: %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	if !store.listCalled {
		t.Fatal("expected ListURLsByUser to be called")
	}

	if !store.listParams.UserID.Valid {
		t.Fatal("list user ID must be non-null")
	}

	if store.listParams.UserID.Int64 != 44 {
		t.Fatalf(
			"list user ID = %d, want 44",
			store.listParams.UserID.Int64,
		)
	}

	if store.listParams.Limit != 7 {
		t.Fatalf(
			"list limit = %d, want 7",
			store.listParams.Limit,
		)
	}

	var response listUserURLsResponse

	if err := json.NewDecoder(
		recorder.Body,
	).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response.URLs) != 1 {
		t.Fatalf(
			"URL count = %d, want 1",
			len(response.URLs),
		)
	}

	got := response.URLs[0]

	if got.ShortCode != "recent1" {
		t.Fatalf(
			"short code = %q, want recent1",
			got.ShortCode,
		)
	}

	if got.OriginalURL != "https://example.com/recent" {
		t.Fatalf(
			"original URL = %q",
			got.OriginalURL,
		)
	}

	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf(
			"created_at = %v, want %v",
			got.CreatedAt,
			createdAt,
		)
	}

	if got.ExpiresAt != nil {
		t.Fatalf(
			"expires_at = %v, want nil",
			got.ExpiresAt,
		)
	}
}

func TestListUserURLsUsesDefaultLimit(t *testing.T) {
	store := &fakeURLStore{}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:    55,
				Email: "owner@example.com",
			}, nil
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

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls",
		nil,
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "valid-session-token",
		},
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	if store.listParams.Limit != defaultURLListLimit {
		t.Fatalf(
			"limit = %d, want %d",
			store.listParams.Limit,
			defaultURLListLimit,
		)
	}
}

func TestListUserURLsRejectsInvalidLimit(t *testing.T) {
	store := &fakeURLStore{}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID: 66,
			}, nil
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

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls?limit=101",
		nil,
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "valid-session-token",
		},
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}

	if store.listCalled {
		t.Fatal("ListURLsByUser must not run for invalid limit")
	}
}
