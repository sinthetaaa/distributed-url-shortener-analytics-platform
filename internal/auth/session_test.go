package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGenerateSessionToken(t *testing.T) {
	token, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken() error = %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}

	if len(raw) != sessionTokenBytes {
		t.Fatalf("session token bytes = %d, want %d", len(raw), sessionTokenBytes)
	}
}

func TestGenerateSessionTokenProducesDifferentTokens(t *testing.T) {
	first, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("first GenerateSessionToken() error = %v", err)
	}

	second, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("second GenerateSessionToken() error = %v", err)
	}

	if first == second {
		t.Fatal("GenerateSessionToken() produced duplicate tokens")
	}
}

func TestHashSessionToken(t *testing.T) {
	token := "test-session-token"

	first := HashSessionToken(token)
	second := HashSessionToken(token)

	if len(first) != sha256.Size {
		t.Fatalf("hash length = %d, want %d", len(first), sha256.Size)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("HashSessionToken() is not deterministic")
	}

	if bytes.Equal(first, []byte(token)) {
		t.Fatal("HashSessionToken() returned plaintext token")
	}
}
