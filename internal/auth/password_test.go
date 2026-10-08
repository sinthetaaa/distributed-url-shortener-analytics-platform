package auth

import (
	"errors"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{
			name:     "valid password",
			password: "correct-horse-battery-staple",
		},
		{
			name:     "too short",
			password: "short",
			wantErr:  ErrPasswordTooShort,
		},
		{
			name:     "maximum bcrypt length",
			password: string(make([]byte, maxPasswordLength)),
		},
		{
			name:     "too long for bcrypt",
			password: string(make([]byte, maxPasswordLength+1)),
			wantErr:  ErrPasswordTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidatePassword() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	password := "correct-horse-battery-staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == password {
		t.Fatal("HashPassword() returned plaintext password")
	}

	if !VerifyPassword(hash, password) {
		t.Fatal("VerifyPassword() rejected correct password")
	}

	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("VerifyPassword() accepted incorrect password")
	}
}

func TestHashPasswordRejectsInvalidPassword(t *testing.T) {
	if _, err := HashPassword("short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("HashPassword() error = %v, want %v", err, ErrPasswordTooShort)
	}
}
