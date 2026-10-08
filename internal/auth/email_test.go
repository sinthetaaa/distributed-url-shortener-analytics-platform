package auth

import (
	"errors"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	got, err := NormalizeEmail("  USER@Example.COM  ")
	if err != nil {
		t.Fatalf("NormalizeEmail() error = %v", err)
	}

	want := "user@example.com"

	if got != want {
		t.Fatalf("NormalizeEmail() = %q, want %q", got, want)
	}
}

func TestNormalizeEmailRejectsInvalidEmail(t *testing.T) {
	if _, err := NormalizeEmail("not-an-email"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("NormalizeEmail() error = %v, want %v", err, ErrInvalidEmail)
	}
}
