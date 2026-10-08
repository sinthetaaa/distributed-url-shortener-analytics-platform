package auth

import (
	"context"
	"errors"
	"time"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultSessionTTL = 7 * 24 * time.Hour

var (
	ErrEmailAlreadyRegistered = errors.New("email is already registered")
	ErrInvalidCredentials     = errors.New("invalid email or password")
	ErrInvalidSession         = errors.New("invalid or expired session")
)

type Store interface {
	CreateUser(
		context.Context,
		database.CreateUserParams,
	) (database.User, error)

	GetUserByEmail(
		context.Context,
		string,
	) (database.User, error)

	GetUserByID(
		context.Context,
		int64,
	) (database.User, error)

	CreateUserSession(
		context.Context,
		database.CreateUserSessionParams,
	) (database.UserSession, error)

	GetUserSessionByTokenHash(
		context.Context,
		[]byte,
	) (database.UserSession, error)

	DeleteUserSession(
		context.Context,
		[]byte,
	) error
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	User      database.User
}

type Service struct {
	store                Store
	now                  func() time.Time
	generateSessionToken func() (string, error)
	sessionTTL           time.Duration
}

func NewService(store Store) *Service {
	return &Service{
		store:                store,
		now:                  time.Now,
		generateSessionToken: GenerateSessionToken,
		sessionTTL:           defaultSessionTTL,
	}
}

func (s *Service) Register(
	ctx context.Context,
	email string,
	password string,
) (database.User, error) {
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil {
		return database.User{}, err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return database.User{}, err
	}

	user, err := s.store.CreateUser(
		ctx,
		database.CreateUserParams{
			Email:        normalizedEmail,
			PasswordHash: passwordHash,
		},
	)
	if err != nil {
		if isDuplicateEmail(err) {
			return database.User{}, ErrEmailAlreadyRegistered
		}

		return database.User{}, err
	}

	return user, nil
}

func (s *Service) Login(
	ctx context.Context,
	email string,
	password string,
) (Session, error) {
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}

	user, err := s.store.GetUserByEmail(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrInvalidCredentials
		}

		return Session{}, err
	}

	if !VerifyPassword(user.PasswordHash, password) {
		return Session{}, ErrInvalidCredentials
	}

	token, err := s.generateSessionToken()
	if err != nil {
		return Session{}, err
	}

	expiresAt := s.now().UTC().Add(s.sessionTTL)

	_, err = s.store.CreateUserSession(
		ctx,
		database.CreateUserSessionParams{
			TokenHash: HashSessionToken(token),
			UserID:    user.ID,
			ExpiresAt: pgtype.Timestamptz{
				Time:  expiresAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		return Session{}, err
	}

	return Session{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      user,
	}, nil
}

func (s *Service) Authenticate(
	ctx context.Context,
	token string,
) (database.User, error) {
	if token == "" {
		return database.User{}, ErrInvalidSession
	}

	session, err := s.store.GetUserSessionByTokenHash(
		ctx,
		HashSessionToken(token),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return database.User{}, ErrInvalidSession
		}

		return database.User{}, err
	}

	user, err := s.store.GetUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return database.User{}, ErrInvalidSession
		}

		return database.User{}, err
	}

	return user, nil
}

func (s *Service) Logout(
	ctx context.Context,
	token string,
) error {
	if token == "" {
		return nil
	}

	return s.store.DeleteUserSession(
		ctx,
		HashSessionToken(token),
	)
}

func isDuplicateEmail(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" &&
		pgErr.ConstraintName == "users_email_key"
}
