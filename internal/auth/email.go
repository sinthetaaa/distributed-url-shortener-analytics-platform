package auth

import (
	"errors"
	"net/mail"
	"strings"
)

var ErrInvalidEmail = errors.New("email must be valid")

func NormalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))

	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return "", ErrInvalidEmail
	}

	return normalized, nil
}
