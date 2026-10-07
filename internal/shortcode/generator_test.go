package shortcode

import (
	"strings"
	"testing"
)

func TestGenerateLength(t *testing.T) {
	code, err := Generate(7)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if len(code) != 7 {
		t.Errorf("expected length 7, got %d", len(code))
	}
}

func TestGenerateUsesBase62Alphabet(t *testing.T) {
	code, err := Generate(100)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	for _, character := range code {
		if !strings.ContainsRune(alphabet, character) {
			t.Errorf("generated character %q is not in Base62 alphabet", character)
		}
	}
}

func TestGenerateRejectsNonPositiveLength(t *testing.T) {
	tests := []int{0, -1}

	for _, length := range tests {
		if _, err := Generate(length); err == nil {
			t.Errorf("expected error for length %d", length)
		}
	}
}
