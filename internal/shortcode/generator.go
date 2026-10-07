package shortcode

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func Generate(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("short code length must be greater than zero")
	}

	code := make([]byte, length)
	alphabetSize := big.NewInt(int64(len(alphabet)))

	for i := range code {
		index, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", fmt.Errorf("generate random short code: %w", err)
		}

		code[i] = alphabet[index.Int64()]
	}

	return string(code), nil
}
