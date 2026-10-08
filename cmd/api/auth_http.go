package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	authpkg "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/auth"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

const sessionCookieName = "shortscale_session"

type authenticationService interface {
	Register(
		context.Context,
		string,
		string,
	) (database.User, error)

	Login(
		context.Context,
		string,
		string,
	) (authpkg.Session, error)

	Authenticate(
		context.Context,
		string,
	) (database.User, error)

	Logout(
		context.Context,
		string,
	) error
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authUserResponse struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

type authenticatedUserContextKey struct{}

func registerHandler(
	logger *slog.Logger,
	service authenticationService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := decodeAuthRequest(w, r, logger)
		if !ok {
			return
		}

		user, err := service.Register(
			r.Context(),
			request.Email,
			request.Password,
		)
		if err != nil {
			switch {
			case errors.Is(err, authpkg.ErrInvalidEmail),
				errors.Is(err, authpkg.ErrPasswordTooShort),
				errors.Is(err, authpkg.ErrPasswordTooLong):
				writeJSONError(
					w,
					logger,
					http.StatusBadRequest,
					err.Error(),
				)

			case errors.Is(err, authpkg.ErrEmailAlreadyRegistered):
				writeJSONError(
					w,
					logger,
					http.StatusConflict,
					"email is already registered",
				)

			default:
				logger.Error(
					"failed to register user",
					"error",
					err,
				)

				writeJSONError(
					w,
					logger,
					http.StatusInternalServerError,
					"internal server error",
				)
			}

			return
		}

		writeJSON(
			w,
			logger,
			http.StatusCreated,
			newAuthUserResponse(user),
		)
	}
}

func loginHandler(
	logger *slog.Logger,
	service authenticationService,
	cookieSecure bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := decodeAuthRequest(w, r, logger)
		if !ok {
			return
		}

		session, err := service.Login(
			r.Context(),
			request.Email,
			request.Password,
		)
		if err != nil {
			if errors.Is(err, authpkg.ErrInvalidCredentials) {
				writeJSONError(
					w,
					logger,
					http.StatusUnauthorized,
					"invalid email or password",
				)
				return
			}

			logger.Error(
				"failed to log in user",
				"error",
				err,
			)

			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		setSessionCookie(
			w,
			session.Token,
			session.ExpiresAt,
			cookieSecure,
		)

		writeJSON(
			w,
			logger,
			http.StatusOK,
			newAuthUserResponse(session.User),
		)
	}
}

func logoutHandler(
	logger *slog.Logger,
	service authenticationService,
	cookieSecure bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil && !errors.Is(err, http.ErrNoCookie) {
			writeJSONError(
				w,
				logger,
				http.StatusBadRequest,
				"invalid session cookie",
			)
			return
		}

		if err == nil {
			if err := service.Logout(
				r.Context(),
				cookie.Value,
			); err != nil {
				logger.Error(
					"failed to log out user",
					"error",
					err,
				)

				writeJSONError(
					w,
					logger,
					http.StatusInternalServerError,
					"internal server error",
				)
				return
			}
		}

		clearSessionCookie(w, cookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

func meHandler(
	logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUserFromContext(r.Context())
		if !ok {
			logger.Error("authenticated user missing from request context")

			writeJSONError(
				w,
				logger,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}

		writeJSON(
			w,
			logger,
			http.StatusOK,
			newAuthUserResponse(user),
		)
	}
}

func authenticationMiddleware(
	logger *slog.Logger,
	service authenticationService,
	cookieSecure bool,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				cookie, err := r.Cookie(sessionCookieName)
				if err != nil {
					if errors.Is(err, http.ErrNoCookie) {
						writeJSONError(
							w,
							logger,
							http.StatusUnauthorized,
							"authentication required",
						)
						return
					}

					writeJSONError(
						w,
						logger,
						http.StatusBadRequest,
						"invalid session cookie",
					)
					return
				}

				user, err := service.Authenticate(
					r.Context(),
					cookie.Value,
				)
				if err != nil {
					if errors.Is(err, authpkg.ErrInvalidSession) {
						clearSessionCookie(w, cookieSecure)

						writeJSONError(
							w,
							logger,
							http.StatusUnauthorized,
							"authentication required",
						)
						return
					}

					logger.Error(
						"failed to authenticate session",
						"error",
						err,
					)

					writeJSONError(
						w,
						logger,
						http.StatusInternalServerError,
						"internal server error",
					)
					return
				}

				ctx := context.WithValue(
					r.Context(),
					authenticatedUserContextKey{},
					user,
				)

				next.ServeHTTP(
					w,
					r.WithContext(ctx),
				)
			},
		)
	}
}

func decodeAuthRequest(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
) (authRequest, bool) {
	var request authRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeJSONError(
			w,
			logger,
			http.StatusBadRequest,
			"invalid request body",
		)
		return authRequest{}, false
	}

	return request, true
}

func newAuthUserResponse(
	user database.User,
) authUserResponse {
	return authUserResponse{
		ID:    user.ID,
		Email: user.Email,
	}
}

func setSessionCookie(
	w http.ResponseWriter,
	token string,
	expiresAt time.Time,
	secure bool,
) {
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		},
	)
}

func clearSessionCookie(
	w http.ResponseWriter,
	secure bool,
) {
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Expires:  time.Unix(1, 0).UTC(),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		},
	)
}

func authenticatedUserFromContext(
	ctx context.Context,
) (database.User, bool) {
	user, ok := ctx.Value(
		authenticatedUserContextKey{},
	).(database.User)

	return user, ok
}
