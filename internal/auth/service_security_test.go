package auth

import (
	"context"
	"errors"
	"testing"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5"
)

func TestServiceLoginConsumesPasswordVerificationForUnknownEmail(
	t *testing.T,
) {
	store := &fakeStore{
		getUserByEmailFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{}, pgx.ErrNoRows
		},
	}

	service := NewService(store)

	verifyCalls := 0
	var verifiedHash string
	var verifiedPassword string

	service.verifyPassword = func(
		hash string,
		password string,
	) bool {
		verifyCalls++
		verifiedHash = hash
		verifiedPassword = password
		return false
	}

	_, err := service.Login(
		context.Background(),
		"missing@example.com",
		"candidate-password",
	)

	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf(
			"Login() error = %v, want %v",
			err,
			ErrInvalidCredentials,
		)
	}

	if verifyCalls != 1 {
		t.Fatalf(
			"password verification calls = %d, want 1",
			verifyCalls,
		)
	}

	if verifiedHash != dummyPasswordHash {
		t.Fatal(
			"unknown-user login did not use dummy bcrypt hash",
		)
	}

	if verifiedPassword != "candidate-password" {
		t.Fatalf(
			"verified password = %q",
			verifiedPassword,
		)
	}
}

func TestServiceLoginUsesSameVerificationPathForExistingUser(
	t *testing.T,
) {
	store := &fakeStore{
		getUserByEmailFn: func(
			context.Context,
			string,
		) (database.User, error) {
			return database.User{
				ID:           9,
				Email:        "user@example.com",
				PasswordHash: "stored-password-hash",
			}, nil
		},
	}

	service := NewService(store)

	verifyCalls := 0

	service.verifyPassword = func(
		hash string,
		password string,
	) bool {
		verifyCalls++

		if hash != "stored-password-hash" {
			t.Fatalf(
				"verification hash = %q",
				hash,
			)
		}

		if password != "wrong-password" {
			t.Fatalf(
				"verification password = %q",
				password,
			)
		}

		return false
	}

	_, err := service.Login(
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

	if verifyCalls != 1 {
		t.Fatalf(
			"password verification calls = %d, want 1",
			verifyCalls,
		)
	}
}
