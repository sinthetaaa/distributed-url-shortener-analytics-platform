package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type fakeStore struct {
	createUserFn                func(context.Context, database.CreateUserParams) (database.User, error)
	getUserByEmailFn            func(context.Context, string) (database.User, error)
	getUserByIDFn               func(context.Context, int64) (database.User, error)
	createUserSessionFn         func(context.Context, database.CreateUserSessionParams) (database.UserSession, error)
	getUserSessionByTokenHashFn func(context.Context, []byte) (database.UserSession, error)
	deleteUserSessionFn         func(context.Context, []byte) error
}

func (f *fakeStore) CreateUser(
	ctx context.Context,
	arg database.CreateUserParams,
) (database.User, error) {
	if f.createUserFn == nil {
		return database.User{}, errors.New("unexpected CreateUser call")
	}

	return f.createUserFn(ctx, arg)
}

func (f *fakeStore) GetUserByEmail(
	ctx context.Context,
	email string,
) (database.User, error) {
	if f.getUserByEmailFn == nil {
		return database.User{}, errors.New("unexpected GetUserByEmail call")
	}

	return f.getUserByEmailFn(ctx, email)
}

func (f *fakeStore) GetUserByID(
	ctx context.Context,
	id int64,
) (database.User, error) {
	if f.getUserByIDFn == nil {
		return database.User{}, errors.New("unexpected GetUserByID call")
	}

	return f.getUserByIDFn(ctx, id)
}

func (f *fakeStore) CreateUserSession(
	ctx context.Context,
	arg database.CreateUserSessionParams,
) (database.UserSession, error) {
	if f.createUserSessionFn == nil {
		return database.UserSession{}, errors.New("unexpected CreateUserSession call")
	}

	return f.createUserSessionFn(ctx, arg)
}

func (f *fakeStore) GetUserSessionByTokenHash(
	ctx context.Context,
	hash []byte,
) (database.UserSession, error) {
	if f.getUserSessionByTokenHashFn == nil {
		return database.UserSession{}, errors.New("unexpected GetUserSessionByTokenHash call")
	}

	return f.getUserSessionByTokenHashFn(ctx, hash)
}

func (f *fakeStore) DeleteUserSession(
	ctx context.Context,
	hash []byte,
) error {
	if f.deleteUserSessionFn == nil {
		return errors.New("unexpected DeleteUserSession call")
	}

	return f.deleteUserSessionFn(ctx, hash)
}

func TestServiceRegister(t *testing.T) {
	store := &fakeStore{
		createUserFn: func(
			_ context.Context,
			arg database.CreateUserParams,
		) (database.User, error) {
			if arg.Email != "user@example.com" {
				t.Fatalf("CreateUser email = %q", arg.Email)
			}

			if arg.PasswordHash == "correct-password" {
				t.Fatal("CreateUser received plaintext password")
			}

			if !VerifyPassword(arg.PasswordHash, "correct-password") {
				t.Fatal("CreateUser received unusable password hash")
			}

			return database.User{
				ID:           42,
				Email:        arg.Email,
				PasswordHash: arg.PasswordHash,
			}, nil
		},
	}

	service := NewService(store)

	user, err := service.Register(
		context.Background(),
		"  USER@Example.COM ",
		"correct-password",
	)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if user.ID != 42 {
		t.Fatalf("Register() user ID = %d, want 42", user.ID)
	}

	if user.Email != "user@example.com" {
		t.Fatalf("Register() email = %q", user.Email)
	}
}

func TestServiceRegisterRejectsDuplicateEmail(t *testing.T) {
	store := &fakeStore{
		createUserFn: func(
			context.Context,
			database.CreateUserParams,
		) (database.User, error) {
			return database.User{}, &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "users_email_key",
			}
		},
	}

	service := NewService(store)

	_, err := service.Register(
		context.Background(),
		"user@example.com",
		"correct-password",
	)

	if !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Fatalf(
			"Register() error = %v, want %v",
			err,
			ErrEmailAlreadyRegistered,
		)
	}
}

func TestServiceLoginCreatesHashedSession(t *testing.T) {
	passwordHash, err := HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	fixedNow := time.Date(
		2026,
		time.October,
		8,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	token := "fixed-test-session-token"

	store := &fakeStore{
		getUserByEmailFn: func(
			_ context.Context,
			email string,
		) (database.User, error) {
			if email != "user@example.com" {
				t.Fatalf("GetUserByEmail email = %q", email)
			}

			return database.User{
				ID:           7,
				Email:        email,
				PasswordHash: passwordHash,
			}, nil
		},
		createUserSessionFn: func(
			_ context.Context,
			arg database.CreateUserSessionParams,
		) (database.UserSession, error) {
			if !bytes.Equal(
				arg.TokenHash,
				HashSessionToken(token),
			) {
				t.Fatal("CreateUserSession received wrong token hash")
			}

			if bytes.Equal(arg.TokenHash, []byte(token)) {
				t.Fatal("CreateUserSession received plaintext token")
			}

			if arg.UserID != 7 {
				t.Fatalf("CreateUserSession user ID = %d", arg.UserID)
			}

			wantExpiry := fixedNow.Add(defaultSessionTTL)

			if !arg.ExpiresAt.Valid ||
				!arg.ExpiresAt.Time.Equal(wantExpiry) {
				t.Fatalf(
					"CreateUserSession expiry = %v, want %v",
					arg.ExpiresAt,
					wantExpiry,
				)
			}

			return database.UserSession{
				TokenHash: arg.TokenHash,
				UserID:    arg.UserID,
				ExpiresAt: arg.ExpiresAt,
			}, nil
		},
	}

	service := NewService(store)
	service.now = func() time.Time {
		return fixedNow
	}
	service.generateSessionToken = func() (string, error) {
		return token, nil
	}

	session, err := service.Login(
		context.Background(),
		"USER@example.com",
		"correct-password",
	)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if session.Token != token {
		t.Fatalf("Login() token = %q, want %q", session.Token, token)
	}

	if session.User.ID != 7 {
		t.Fatalf("Login() user ID = %d, want 7", session.User.ID)
	}

	wantExpiry := fixedNow.Add(defaultSessionTTL)

	if !session.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf(
			"Login() expiry = %v, want %v",
			session.ExpiresAt,
			wantExpiry,
		)
	}
}

func TestServiceLoginRejectsUnknownEmail(t *testing.T) {
	store := &fakeStore{
		getUserByEmailFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{}, pgx.ErrNoRows
		},
	}

	service := NewService(store)

	_, err := service.Login(
		context.Background(),
		"missing@example.com",
		"correct-password",
	)

	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf(
			"Login() error = %v, want %v",
			err,
			ErrInvalidCredentials,
		)
	}
}

func TestServiceLoginRejectsWrongPassword(t *testing.T) {
	passwordHash, err := HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	store := &fakeStore{
		getUserByEmailFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:           7,
				Email:        "user@example.com",
				PasswordHash: passwordHash,
			}, nil
		},
	}

	service := NewService(store)

	_, err = service.Login(
		context.Background(),
		"user@example.com",
		"wrong-password",
	)

	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf(
			"Login() error = %v, want %v",
			err,
			ErrInvalidCredentials,
		)
	}
}

func TestServiceAuthenticate(t *testing.T) {
	token := "valid-session-token"
	expectedHash := HashSessionToken(token)

	store := &fakeStore{
		getUserSessionByTokenHashFn: func(
			_ context.Context,
			hash []byte,
		) (database.UserSession, error) {
			if !bytes.Equal(hash, expectedHash) {
				t.Fatal("GetUserSessionByTokenHash received wrong hash")
			}

			return database.UserSession{
				UserID: 11,
				ExpiresAt: pgtype.Timestamptz{
					Time:  time.Now().Add(time.Hour),
					Valid: true,
				},
			}, nil
		},
		getUserByIDFn: func(
			_ context.Context,
			id int64,
		) (database.User, error) {
			if id != 11 {
				t.Fatalf("GetUserByID id = %d, want 11", id)
			}

			return database.User{
				ID:    11,
				Email: "user@example.com",
			}, nil
		},
	}

	service := NewService(store)

	user, err := service.Authenticate(
		context.Background(),
		token,
	)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if user.ID != 11 {
		t.Fatalf("Authenticate() user ID = %d, want 11", user.ID)
	}
}

func TestServiceAuthenticateRejectsMissingSession(t *testing.T) {
	store := &fakeStore{
		getUserSessionByTokenHashFn: func(
			context.Context,
			[]byte,
		) (database.UserSession, error) {
			return database.UserSession{}, pgx.ErrNoRows
		},
	}

	service := NewService(store)

	_, err := service.Authenticate(
		context.Background(),
		"expired-token",
	)

	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf(
			"Authenticate() error = %v, want %v",
			err,
			ErrInvalidSession,
		)
	}
}

func TestServiceLogoutDeletesHashedToken(t *testing.T) {
	token := "logout-session-token"
	expectedHash := HashSessionToken(token)

	store := &fakeStore{
		deleteUserSessionFn: func(
			_ context.Context,
			hash []byte,
		) error {
			if !bytes.Equal(hash, expectedHash) {
				t.Fatal("DeleteUserSession received wrong token hash")
			}

			return nil
		},
	}

	service := NewService(store)

	if err := service.Logout(
		context.Background(),
		token,
	); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
}

func TestServiceLogoutWithoutTokenIsIdempotent(t *testing.T) {
	service := NewService(&fakeStore{})

	if err := service.Logout(
		context.Background(),
		"",
	); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
}
