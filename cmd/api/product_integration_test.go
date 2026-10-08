package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	authpkg "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/auth"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBackendProductFlow(t *testing.T) {
	databaseURL := os.Getenv("SHORTSCALE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SHORTSCALE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	_, err = pool.Exec(
		ctx,
		`TRUNCATE TABLE
			user_sessions,
			redirect_events,
			redirect_daily_counts,
			urls,
			users
		RESTART IDENTITY CASCADE`,
	)
	if err != nil {
		t.Fatalf("reset product test database: %v", err)
	}

	queries := database.New(pool)
	authService := authpkg.NewService(queries)

	router := newRouterWithDependenciesMetricsAndAuth(
		authTestLogger(),
		pool,
		queries,
		queries,
		newLocalTokenBucketLimiter(
			createURLRateLimitCapacity,
			createURLRateLimitRefillPerSecond,
		),
		noopRedirectEventRecorder{},
		queries,
		nil,
		authService,
		false,
	)

	firstUser := registerProductTestUser(
		t,
		router,
		"owner@example.com",
		"correct-password-123",
	)

	firstSession := loginProductTestUser(
		t,
		router,
		"owner@example.com",
		"correct-password-123",
	)

	meRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/auth/me",
		"",
		firstSession,
	)

	if meRecorder.Code != http.StatusOK {
		t.Fatalf(
			"GET /auth/me status = %d, want %d: %s",
			meRecorder.Code,
			http.StatusOK,
			meRecorder.Body.String(),
		)
	}

	var me authUserResponse
	decodeProductTestJSON(
		t,
		meRecorder,
		&me,
	)

	if me.ID != firstUser.ID ||
		me.Email != firstUser.Email {
		t.Fatalf(
			"GET /auth/me user = %+v, want %+v",
			me,
			firstUser,
		)
	}

	createRecorder := productTestRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/urls",
		`{"url":"https://example.com/product-flow"}`,
		firstSession,
	)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf(
			"POST /urls status = %d, want %d: %s",
			createRecorder.Code,
			http.StatusCreated,
			createRecorder.Body.String(),
		)
	}

	var created createURLResponse
	decodeProductTestJSON(
		t,
		createRecorder,
		&created,
	)

	if created.ShortCode == "" {
		t.Fatal("created short code must not be empty")
	}

	if created.OriginalURL !=
		"https://example.com/product-flow" {
		t.Fatalf(
			"created original URL = %q",
			created.OriginalURL,
		)
	}

	var persistedOwnerID int64

	err = pool.QueryRow(
		ctx,
		`SELECT user_id
		FROM urls
		WHERE short_code = $1`,
		created.ShortCode,
	).Scan(&persistedOwnerID)
	if err != nil {
		t.Fatalf(
			"read persisted URL owner: %v",
			err,
		)
	}

	if persistedOwnerID != firstUser.ID {
		t.Fatalf(
			"persisted URL owner = %d, want %d",
			persistedOwnerID,
			firstUser.ID,
		)
	}

	listRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/urls",
		"",
		firstSession,
	)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf(
			"GET /urls status = %d, want %d: %s",
			listRecorder.Code,
			http.StatusOK,
			listRecorder.Body.String(),
		)
	}

	var firstUserURLs listUserURLsResponse
	decodeProductTestJSON(
		t,
		listRecorder,
		&firstUserURLs,
	)

	if len(firstUserURLs.URLs) != 1 {
		t.Fatalf(
			"first user URL count = %d, want 1",
			len(firstUserURLs.URLs),
		)
	}

	if firstUserURLs.URLs[0].ShortCode !=
		created.ShortCode {
		t.Fatalf(
			"listed short code = %q, want %q",
			firstUserURLs.URLs[0].ShortCode,
			created.ShortCode,
		)
	}

	analyticsRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/urls/"+created.ShortCode+"/analytics",
		"",
		firstSession,
	)

	if analyticsRecorder.Code != http.StatusOK {
		t.Fatalf(
			"owner analytics status = %d, want %d: %s",
			analyticsRecorder.Code,
			http.StatusOK,
			analyticsRecorder.Body.String(),
		)
	}

	var analytics redirectAnalyticsResponse
	decodeProductTestJSON(
		t,
		analyticsRecorder,
		&analytics,
	)

	if analytics.ShortCode != created.ShortCode {
		t.Fatalf(
			"analytics short code = %q, want %q",
			analytics.ShortCode,
			created.ShortCode,
		)
	}

	if analytics.TotalRedirects != 0 {
		t.Fatalf(
			"analytics redirects = %d, want 0",
			analytics.TotalRedirects,
		)
	}

	redirectRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/"+created.ShortCode,
		"",
		nil,
	)

	if redirectRecorder.Code != http.StatusFound {
		t.Fatalf(
			"public redirect status = %d, want %d",
			redirectRecorder.Code,
			http.StatusFound,
		)
	}

	if location := redirectRecorder.Header().Get("Location"); location != "https://example.com/product-flow" {
		t.Fatalf(
			"public redirect Location = %q",
			location,
		)
	}

	registerProductTestUser(
		t,
		router,
		"second@example.com",
		"second-password-123",
	)

	secondSession := loginProductTestUser(
		t,
		router,
		"second@example.com",
		"second-password-123",
	)

	secondListRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/urls",
		"",
		secondSession,
	)

	if secondListRecorder.Code != http.StatusOK {
		t.Fatalf(
			"second user GET /urls status = %d: %s",
			secondListRecorder.Code,
			secondListRecorder.Body.String(),
		)
	}

	var secondUserURLs listUserURLsResponse
	decodeProductTestJSON(
		t,
		secondListRecorder,
		&secondUserURLs,
	)

	if len(secondUserURLs.URLs) != 0 {
		t.Fatalf(
			"second user URL count = %d, want 0",
			len(secondUserURLs.URLs),
		)
	}

	forbiddenAnalyticsRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/urls/"+created.ShortCode+"/analytics",
		"",
		secondSession,
	)

	if forbiddenAnalyticsRecorder.Code !=
		http.StatusNotFound {
		t.Fatalf(
			"other-user analytics status = %d, want %d: %s",
			forbiddenAnalyticsRecorder.Code,
			http.StatusNotFound,
			forbiddenAnalyticsRecorder.Body.String(),
		)
	}

	logoutRecorder := productTestRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/auth/logout",
		"",
		firstSession,
	)

	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf(
			"logout status = %d, want %d: %s",
			logoutRecorder.Code,
			http.StatusNoContent,
			logoutRecorder.Body.String(),
		)
	}

	revokedSessionRecorder := productTestRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/auth/me",
		"",
		firstSession,
	)

	if revokedSessionRecorder.Code !=
		http.StatusUnauthorized {
		t.Fatalf(
			"revoked session status = %d, want %d: %s",
			revokedSessionRecorder.Code,
			http.StatusUnauthorized,
			revokedSessionRecorder.Body.String(),
		)
	}

	var remainingSessions int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		FROM user_sessions
		WHERE user_id = $1`,
		firstUser.ID,
	).Scan(&remainingSessions)
	if err != nil {
		t.Fatalf(
			"count first-user sessions: %v",
			err,
		)
	}

	if remainingSessions != 0 {
		t.Fatalf(
			"first-user session count after logout = %d, want 0",
			remainingSessions,
		)
	}
}

func registerProductTestUser(
	t *testing.T,
	router http.Handler,
	email string,
	password string,
) authUserResponse {
	t.Helper()

	recorder := productTestRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/auth/register",
		`{"email":"`+email+`","password":"`+password+`"}`,
		nil,
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf(
			"register %s status = %d, want %d: %s",
			email,
			recorder.Code,
			http.StatusCreated,
			recorder.Body.String(),
		)
	}

	var user authUserResponse
	decodeProductTestJSON(t, recorder, &user)

	if user.ID == 0 {
		t.Fatalf(
			"registered user %s has zero ID",
			email,
		)
	}

	if user.Email != email {
		t.Fatalf(
			"registered email = %q, want %q",
			user.Email,
			email,
		)
	}

	return user
}

func loginProductTestUser(
	t *testing.T,
	router http.Handler,
	email string,
	password string,
) *http.Cookie {
	t.Helper()

	recorder := productTestRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`,
		nil,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"login %s status = %d, want %d: %s",
			email,
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			if cookie.Value == "" {
				t.Fatal(
					"session cookie value must not be empty",
				)
			}

			if !cookie.HttpOnly {
				t.Fatal(
					"session cookie must be HttpOnly",
				)
			}

			return cookie
		}
	}

	t.Fatal("login did not return session cookie")
	return nil
}

func productTestRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
	body string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()

	var request *http.Request

	if body == "" {
		request = httptest.NewRequest(
			method,
			path,
			nil,
		)
	} else {
		request = httptest.NewRequest(
			method,
			path,
			strings.NewReader(body),
		)

		request.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	if cookie != nil {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}

func decodeProductTestJSON(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	value any,
) {
	t.Helper()

	if err := json.NewDecoder(
		recorder.Body,
	).Decode(value); err != nil {
		t.Fatalf(
			"decode response JSON: %v; body=%q",
			err,
			recorder.Body.String(),
		)
	}
}
