package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authpkg "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/auth"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

type fakeAuthenticationService struct {
	registerFn     func(context.Context, string, string) (database.User, error)
	loginFn        func(context.Context, string, string) (authpkg.Session, error)
	authenticateFn func(context.Context, string) (database.User, error)
	logoutFn       func(context.Context, string) error
}

func (f *fakeAuthenticationService) Register(
	ctx context.Context,
	email string,
	password string,
) (database.User, error) {
	if f.registerFn == nil {
		return database.User{}, errors.New("unexpected Register call")
	}

	return f.registerFn(ctx, email, password)
}

func (f *fakeAuthenticationService) Login(
	ctx context.Context,
	email string,
	password string,
) (authpkg.Session, error) {
	if f.loginFn == nil {
		return authpkg.Session{}, errors.New("unexpected Login call")
	}

	return f.loginFn(ctx, email, password)
}

func (f *fakeAuthenticationService) Authenticate(
	ctx context.Context,
	token string,
) (database.User, error) {
	if f.authenticateFn == nil {
		return database.User{}, errors.New("unexpected Authenticate call")
	}

	return f.authenticateFn(ctx, token)
}

func (f *fakeAuthenticationService) Logout(
	ctx context.Context,
	token string,
) error {
	if f.logoutFn == nil {
		return errors.New("unexpected Logout call")
	}

	return f.logoutFn(ctx, token)
}

func authTestLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)
}

func TestRegisterHandler(t *testing.T) {
	service := &fakeAuthenticationService{
		registerFn: func(
			_ context.Context,
			email string,
			password string,
		) (database.User, error) {
			if email != "user@example.com" {
				t.Fatalf("Register email = %q", email)
			}

			if password != "correct-password" {
				t.Fatalf("Register password = %q", password)
			}

			return database.User{
				ID:    12,
				Email: "user@example.com",
			}, nil
		},
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(
			`{"email":"user@example.com","password":"correct-password"}`,
		),
	)

	recorder := httptest.NewRecorder()

	registerHandler(
		authTestLogger(),
		service,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusCreated,
		)
	}

	var response authUserResponse

	if err := json.NewDecoder(
		recorder.Body,
	).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != 12 ||
		response.Email != "user@example.com" {
		t.Fatalf("response = %+v", response)
	}
}

func TestRegisterHandlerDuplicateEmail(t *testing.T) {
	service := &fakeAuthenticationService{
		registerFn: func(
			context.Context,
			string,
			string,
		) (database.User, error) {
			return database.User{},
				authpkg.ErrEmailAlreadyRegistered
		},
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(
			`{"email":"user@example.com","password":"correct-password"}`,
		),
	)

	recorder := httptest.NewRecorder()

	registerHandler(
		authTestLogger(),
		service,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusConflict,
		)
	}
}

func TestLoginHandlerSetsSecureSessionCookie(t *testing.T) {
	expiry := time.Date(
		2026,
		time.October,
		15,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	service := &fakeAuthenticationService{
		loginFn: func(
			context.Context,
			string,
			string,
		) (authpkg.Session, error) {
			return authpkg.Session{
				Token:     "secret-session-token",
				ExpiresAt: expiry,
				User: database.User{
					ID:    9,
					Email: "user@example.com",
				},
			}, nil
		},
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(
			`{"email":"user@example.com","password":"correct-password"}`,
		),
	)

	recorder := httptest.NewRecorder()

	loginHandler(
		authTestLogger(),
		service,
		true,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf(
			"cookie count = %d, want 1",
			len(cookies),
		)
	}

	cookie := cookies[0]

	if cookie.Name != sessionCookieName {
		t.Fatalf("cookie name = %q", cookie.Name)
	}

	if cookie.Value != "secret-session-token" {
		t.Fatal("session cookie has wrong value")
	}

	if !cookie.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}

	if !cookie.Secure {
		t.Fatal("session cookie must be Secure")
	}

	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf(
			"SameSite = %v, want Lax",
			cookie.SameSite,
		)
	}

	if cookie.Path != "/" {
		t.Fatalf(
			"cookie path = %q, want /",
			cookie.Path,
		)
	}

	if !cookie.Expires.Equal(expiry) {
		t.Fatalf(
			"expiry = %v, want %v",
			cookie.Expires,
			expiry,
		)
	}
}

func TestLoginHandlerRejectsInvalidCredentials(t *testing.T) {
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

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(
			`{"email":"user@example.com","password":"wrong-password"}`,
		),
	)

	recorder := httptest.NewRecorder()

	loginHandler(
		authTestLogger(),
		service,
		false,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusUnauthorized,
		)
	}
}

func TestLogoutHandlerDeletesSessionAndClearsCookie(t *testing.T) {
	var receivedToken string

	service := &fakeAuthenticationService{
		logoutFn: func(
			_ context.Context,
			token string,
		) error {
			receivedToken = token
			return nil
		},
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/logout",
		nil,
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "logout-token",
		},
	)

	recorder := httptest.NewRecorder()

	logoutHandler(
		authTestLogger(),
		service,
		true,
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if receivedToken != "logout-token" {
		t.Fatalf(
			"Logout token = %q",
			receivedToken,
		)
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf(
			"cookie count = %d, want 1",
			len(cookies),
		)
	}

	if cookies[0].MaxAge >= 0 {
		t.Fatalf(
			"cleared cookie MaxAge = %d",
			cookies[0].MaxAge,
		)
	}
}

func TestAuthenticationMiddlewareRejectsMissingCookie(t *testing.T) {
	service := &fakeAuthenticationService{}

	next := http.HandlerFunc(
		func(
			http.ResponseWriter,
			*http.Request,
		) {
			t.Fatal("next handler should not run")
		},
	)

	handler := authenticationMiddleware(
		authTestLogger(),
		service,
		false,
	)(next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/protected",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusUnauthorized,
		)
	}
}

func TestAuthenticationMiddlewareAddsUserToContext(t *testing.T) {
	service := &fakeAuthenticationService{
		authenticateFn: func(
			_ context.Context,
			token string,
		) (database.User, error) {
			if token != "valid-token" {
				t.Fatalf(
					"Authenticate token = %q",
					token,
				)
			}

			return database.User{
				ID:    27,
				Email: "user@example.com",
			}, nil
		},
	}

	next := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			user, ok := authenticatedUserFromContext(
				r.Context(),
			)

			if !ok {
				t.Fatal("authenticated user missing")
			}

			if user.ID != 27 {
				t.Fatalf(
					"user ID = %d, want 27",
					user.ID,
				)
			}

			w.WriteHeader(http.StatusNoContent)
		},
	)

	handler := authenticationMiddleware(
		authTestLogger(),
		service,
		false,
	)(next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/protected",
		nil,
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "valid-token",
		},
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}
}

func TestAuthenticationMiddlewareRejectsInvalidSession(
	t *testing.T,
) {
	service := &fakeAuthenticationService{
		authenticateFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{},
				authpkg.ErrInvalidSession
		},
	}

	next := http.HandlerFunc(
		func(
			http.ResponseWriter,
			*http.Request,
		) {
			t.Fatal("next handler should not run")
		},
	)

	handler := authenticationMiddleware(
		authTestLogger(),
		service,
		true,
	)(next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/protected",
		nil,
	)

	request.AddCookie(
		&http.Cookie{
			Name:  sessionCookieName,
			Value: "expired-token",
		},
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusUnauthorized,
		)
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf(
			"cookie count = %d, want 1",
			len(cookies),
		)
	}

	if cookies[0].MaxAge >= 0 {
		t.Fatal("invalid session cookie was not cleared")
	}
}

func TestMeHandler(t *testing.T) {
	user := database.User{
		ID:    33,
		Email: "user@example.com",
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/me",
		nil,
	)

	ctx := context.WithValue(
		request.Context(),
		authenticatedUserContextKey{},
		user,
	)

	request = request.WithContext(ctx)

	recorder := httptest.NewRecorder()

	meHandler(
		authTestLogger(),
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	var response authUserResponse

	if err := json.NewDecoder(
		recorder.Body,
	).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != 33 ||
		response.Email != "user@example.com" {
		t.Fatalf("response = %+v", response)
	}
}
