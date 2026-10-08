package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const sessionTokenBytes = 32

func GenerateSessionToken() (string, error) {
	raw := make([]byte, sessionTokenBytes)

	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	hash := make([]byte, len(sum))
	copy(hash, sum[:])

	return hash
}
