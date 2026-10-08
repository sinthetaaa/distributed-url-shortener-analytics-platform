package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

type fakeDatabasePinger struct {
	err error
}

func (f fakeDatabasePinger) Ping(context.Context) error {
	return f.err
}

type fakeURLStore struct {
	created           database.Url
	createErr         error
	createErrors      []error
	createParams      database.CreateURLParams
	createCalled      bool
	createOwnedParams database.CreateOwnedURLParams
	createOwnedCalled bool
	attempts          int
	listed            []database.Url
	listErr           error
	listParams        database.ListURLsByUserParams
	listCalled        bool

	found      database.Url
	findErr    error
	findCode   string
	findCalled bool
}

func (f *fakeURLStore) CreateURL(_ context.Context, params database.CreateURLParams) (database.Url, error) {
	f.createCalled = true
	f.createParams = params
	f.attempts++

	if len(f.createErrors) >= f.attempts {
		if err := f.createErrors[f.attempts-1]; err != nil {
			return database.Url{}, err
		}
	}

	if f.createErr != nil {
		return database.Url{}, f.createErr
	}

	created := f.created
	created.ShortCode = params.ShortCode
	created.OriginalUrl = params.OriginalUrl
	created.ExpiresAt = params.ExpiresAt

	return created, nil
}

func (f *fakeURLStore) CreateOwnedURL(
	_ context.Context,
	params database.CreateOwnedURLParams,
) (database.Url, error) {
	f.createOwnedCalled = true
	f.createOwnedParams = params
	f.attempts++

	if len(f.createErrors) >= f.attempts {
		if err := f.createErrors[f.attempts-1]; err != nil {
			return database.Url{}, err
		}
	}

	if f.createErr != nil {
		return database.Url{}, f.createErr
	}

	created := f.created
	created.ShortCode = params.ShortCode
	created.OriginalUrl = params.OriginalUrl
	created.ExpiresAt = params.ExpiresAt
	created.UserID = params.UserID

	return created, nil
}

func (f *fakeURLStore) ListURLsByUser(
	_ context.Context,
	params database.ListURLsByUserParams,
) ([]database.Url, error) {
	f.listCalled = true
	f.listParams = params

	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.listed, nil
}

func (f *fakeURLStore) GetURLByShortCode(_ context.Context, shortCode string) (database.Url, error) {
	f.findCalled = true
	f.findCode = shortCode

	if f.findErr != nil {
		return database.Url{}, f.findErr
	}

	return f.found, nil
}

func newTestRouter(databasePinger databasePinger, store *fakeURLStore) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRouter(logger, databasePinger, store, store)
}

func TestHealthLive(t *testing.T) {
	router := newTestRouter(fakeDatabasePinger{}, &fakeURLStore{})

	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	expectedBody := `{"status":"ok"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestHealthReadyWhenDatabaseAvailable(t *testing.T) {
	router := newTestRouter(fakeDatabasePinger{}, &fakeURLStore{})

	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	expectedBody := `{"status":"ok"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestHealthReadyWhenDatabaseUnavailable(t *testing.T) {
	router := newTestRouter(
		fakeDatabasePinger{err: errors.New("database unavailable")},
		&fakeURLStore{},
	)

	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}

	expectedBody := `{"status":"unavailable"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestCreateURL(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"https://example.com/article"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if !creator.createCalled {
		t.Fatal("expected CreateURL to be called")
	}

	if creator.createParams.OriginalUrl != "https://example.com/article" {
		t.Errorf("expected original URL %q, got %q", "https://example.com/article", creator.createParams.OriginalUrl)
	}

	if len(creator.createParams.ShortCode) != shortCodeLength {
		t.Errorf("expected short code length %d, got %d", shortCodeLength, len(creator.createParams.ShortCode))
	}

	if creator.createParams.ExpiresAt.Valid {
		t.Error("expected expires_at to be NULL")
	}

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	expectedBody := `{"short_code":"` + creator.createParams.ShortCode + `","original_url":"https://example.com/article"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestCreateURLAcceptsHTTP(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"http://example.com"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if !creator.createCalled {
		t.Fatal("expected CreateURL to be called")
	}
}

func TestCreateURLRejectsMalformedJSON(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	if creator.createCalled {
		t.Error("expected CreateURL not to be called")
	}
}

func TestCreateURLRejectsMissingURL(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	if creator.createCalled {
		t.Error("expected CreateURL not to be called")
	}
}

func TestCreateURLRejectsRelativeURL(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"/article"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	if creator.createCalled {
		t.Error("expected CreateURL not to be called")
	}
}

func TestCreateURLRejectsUnsupportedScheme(t *testing.T) {
	creator := &fakeURLStore{}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"ftp://example.com/file"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	if creator.createCalled {
		t.Error("expected CreateURL not to be called")
	}
}

func TestCreateURLWhenDatabaseFails(t *testing.T) {
	creator := &fakeURLStore{
		createErr: errors.New("database unavailable"),
	}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"https://example.com"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	expectedBody := `{"error":"internal server error"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestCreateURLRetriesShortCodeCollision(t *testing.T) {
	collision := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "urls_short_code_key",
	}

	creator := &fakeURLStore{
		createErrors: []error{collision, nil},
	}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"https://example.com"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if creator.attempts != 2 {
		t.Errorf("expected 2 creation attempts, got %d", creator.attempts)
	}
}

func TestCreateURLStopsAfterMaximumShortCodeCollisions(t *testing.T) {
	collision := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "urls_short_code_key",
	}

	collisions := make([]error, maxShortCodeAttempts)
	for i := range collisions {
		collisions[i] = collision
	}

	creator := &fakeURLStore{
		createErrors: collisions,
	}
	router := newTestRouter(fakeDatabasePinger{}, creator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/urls",
		strings.NewReader(`{"url":"https://example.com"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusInternalServerError,
			recorder.Code,
		)
	}

	if creator.attempts != maxShortCodeAttempts {
		t.Errorf(
			"expected %d creation attempts, got %d",
			maxShortCodeAttempts,
			creator.attempts,
		)
	}

	expectedBody := `{"error":"internal server error"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestRedirectURL(t *testing.T) {
	store := &fakeURLStore{
		found: database.Url{
			ShortCode:   "3ZB9CeC",
			OriginalUrl: "https://example.com/article",
		},
	}
	router := newTestRouter(fakeDatabasePinger{}, store)

	request := httptest.NewRequest(http.MethodGet, "/3ZB9CeC", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, recorder.Code)
	}

	if !store.findCalled {
		t.Fatal("expected GetURLByShortCode to be called")
	}

	if store.findCode != "3ZB9CeC" {
		t.Errorf("expected short code %q, got %q", "3ZB9CeC", store.findCode)
	}

	location := recorder.Header().Get("Location")
	if location != "https://example.com/article" {
		t.Errorf("expected Location %q, got %q", "https://example.com/article", location)
	}
}

func TestRedirectURLWhenShortCodeNotFound(t *testing.T) {
	store := &fakeURLStore{
		findErr: pgx.ErrNoRows,
	}
	router := newTestRouter(fakeDatabasePinger{}, store)

	request := httptest.NewRequest(http.MethodGet, "/doesnotexist", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	if !store.findCalled {
		t.Fatal("expected GetURLByShortCode to be called")
	}

	expectedBody := `{"error":"short URL not found"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestRedirectURLWhenDatabaseFails(t *testing.T) {
	store := &fakeURLStore{
		findErr: errors.New("database unavailable"),
	}
	router := newTestRouter(fakeDatabasePinger{}, store)

	request := httptest.NewRequest(http.MethodGet, "/3ZB9CeC", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusInternalServerError,
			recorder.Code,
		)
	}

	expectedBody := `{"error":"internal server error"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, actualBody)
	}
}

func TestHealthLiveMethodNotAllowed(t *testing.T) {
	router := newTestRouter(fakeDatabasePinger{}, &fakeURLStore{})

	request := httptest.NewRequest(http.MethodPost, "/health/live", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
	}
}
