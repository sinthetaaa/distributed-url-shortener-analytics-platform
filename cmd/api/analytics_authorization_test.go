package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type fakeOwnedAnalyticsReader struct {
	ownedURL        database.Url
	ownedURLErr     error
	ownedURLParams  database.GetURLByShortCodeForUserParams
	ownedURLCalled  bool
	publicURL       database.Url
	publicURLErr    error
	publicURLCalled bool
	summary         database.GetRedirectAnalyticsSummaryRow
	summaryCalled   bool
	daily           []database.GetDailyRedirectCountsRow
	dailyCalled     bool
}

func (f *fakeOwnedAnalyticsReader) GetURLByShortCodeForUser(
	_ context.Context,
	params database.GetURLByShortCodeForUserParams,
) (database.Url, error) {
	f.ownedURLCalled = true
	f.ownedURLParams = params

	if f.ownedURLErr != nil {
		return database.Url{}, f.ownedURLErr
	}

	return f.ownedURL, nil
}

func (f *fakeOwnedAnalyticsReader) GetURLByShortCode(
	context.Context,
	string,
) (database.Url, error) {
	f.publicURLCalled = true

	if f.publicURLErr != nil {
		return database.Url{}, f.publicURLErr
	}

	return f.publicURL, nil
}

func (f *fakeOwnedAnalyticsReader) GetRedirectAnalyticsSummary(
	context.Context,
	string,
) (database.GetRedirectAnalyticsSummaryRow, error) {
	f.summaryCalled = true
	return f.summary, nil
}

func (f *fakeOwnedAnalyticsReader) GetDailyRedirectCounts(
	context.Context,
	database.GetDailyRedirectCountsParams,
) ([]database.GetDailyRedirectCountsRow, error) {
	f.dailyCalled = true
	return f.daily, nil
}

func newAnalyticsAuthorizationRouter(
	t *testing.T,
	reader redirectAnalyticsReader,
	service authenticationService,
) http.Handler {
	t.Helper()

	store := &fakeURLStore{}

	return newRouterWithDependenciesMetricsAndAuth(
		authTestLogger(),
		fakeDatabasePinger{},
		store,
		store,
		newLocalTokenBucketLimiter(
			createURLRateLimitCapacity,
			createURLRateLimitRefillPerSecond,
		),
		noopRedirectEventRecorder{},
		reader,
		nil,
		service,
		false,
	)
}

func TestAnalyticsRequiresAuthentication(t *testing.T) {
	reader := &fakeOwnedAnalyticsReader{}

	router := newAnalyticsAuthorizationRouter(
		t,
		reader,
		&fakeAuthenticationService{},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/abc1234/analytics",
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

	if reader.ownedURLCalled {
		t.Fatal(
			"ownership lookup must not run without authentication",
		)
	}

	if reader.summaryCalled || reader.dailyCalled {
		t.Fatal(
			"analytics queries must not run without authentication",
		)
	}
}

func TestAnalyticsAllowsOwner(t *testing.T) {
	reader := &fakeOwnedAnalyticsReader{
		ownedURL: database.Url{
			ShortCode: "abc1234",
			UserID: pgtype.Int8{
				Int64: 77,
				Valid: true,
			},
		},
		publicURL: database.Url{
			ShortCode: "abc1234",
			UserID: pgtype.Int8{
				Int64: 77,
				Valid: true,
			},
		},
		summary: database.GetRedirectAnalyticsSummaryRow{},
		daily:   []database.GetDailyRedirectCountsRow{},
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:    77,
				Email: "owner@example.com",
			}, nil
		},
	}

	router := newAnalyticsAuthorizationRouter(
		t,
		reader,
		service,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/abc1234/analytics",
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

	if !reader.ownedURLCalled {
		t.Fatal("expected ownership lookup")
	}

	if reader.ownedURLParams.ShortCode != "abc1234" {
		t.Fatalf(
			"ownership short code = %q",
			reader.ownedURLParams.ShortCode,
		)
	}

	if !reader.ownedURLParams.UserID.Valid ||
		reader.ownedURLParams.UserID.Int64 != 77 {
		t.Fatalf(
			"ownership user ID = %+v",
			reader.ownedURLParams.UserID,
		)
	}

	if !reader.summaryCalled || !reader.dailyCalled {
		t.Fatal(
			"expected analytics queries after authorization",
		)
	}
}

func TestAnalyticsHidesOtherUsersURL(t *testing.T) {
	reader := &fakeOwnedAnalyticsReader{
		ownedURLErr: pgx.ErrNoRows,
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:    77,
				Email: "owner@example.com",
			}, nil
		},
	}

	router := newAnalyticsAuthorizationRouter(
		t,
		reader,
		service,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/someone-elses-code/analytics",
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

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}

	if reader.publicURLCalled ||
		reader.summaryCalled ||
		reader.dailyCalled {
		t.Fatal(
			"analytics data must not be queried after ownership denial",
		)
	}
}

func TestAnalyticsHidesMissingURL(t *testing.T) {
	reader := &fakeOwnedAnalyticsReader{
		ownedURLErr: pgx.ErrNoRows,
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID: 88,
			}, nil
		},
	}

	router := newAnalyticsAuthorizationRouter(
		t,
		reader,
		service,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/missing1/analytics",
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

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}
}

func TestAnalyticsOwnershipDatabaseFailure(t *testing.T) {
	reader := &fakeOwnedAnalyticsReader{
		ownedURLErr: context.DeadlineExceeded,
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID: 99,
			}, nil
		},
	}

	router := newAnalyticsAuthorizationRouter(
		t,
		reader,
		service,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/abc1234/analytics",
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

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}
}

func TestPublicRedirectStillDoesNotRequireAuthentication(
	t *testing.T,
) {
	store := &fakeURLStore{
		found: database.Url{
			ShortCode:   "public1",
			OriginalUrl: "https://example.com/public",
		},
	}

	reader := &fakeOwnedAnalyticsReader{}

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
		reader,
		nil,
		&fakeAuthenticationService{},
		false,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/public1",
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

	if location := recorder.Header().Get("Location"); location != "https://example.com/public" {
		t.Fatalf(
			"Location = %q",
			location,
		)
	}
}

func TestAnalyticsOwnerWithDefaultWindowStillWorks(
	t *testing.T,
) {
	reader := &fakeOwnedAnalyticsReader{
		ownedURL: database.Url{
			ShortCode: "abc1234",
		},
		publicURL: database.Url{
			ShortCode: "abc1234",
		},
		daily: []database.GetDailyRedirectCountsRow{
			{
				Day: pgtype.Date{
					Time: time.Date(
						2026,
						time.October,
						8,
						0,
						0,
						0,
						0,
						time.UTC,
					),
					Valid: true,
				},
				Redirects: 3,
			},
		},
	}

	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID: 101,
			}, nil
		},
	}

	router := newAnalyticsAuthorizationRouter(
		t,
		reader,
		service,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/urls/abc1234/analytics",
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
}
