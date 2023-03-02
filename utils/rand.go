package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
)

// GenerateRandHex generates a random hex string of given length in hex format
func GenerateRandHex(length int) (string, error) {
	if length%2 != 0 {
		return "", fmt.Errorf("please provide an even number. Hex strings cannot be unevenly long.")
	}

	b := make([]byte, length/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

var letters = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ!@#$%^&*()_+")

// GenerateRandString generates a random string of given length
func GenerateRandString(length int) (string, error) {
	if length < 1 {
		return "", errors.New("please provide a postive number")
	}

	b := make([]rune, length)
	for i := range b {
		r, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}

		b[i] = letters[r.Int64()]
	}

	return string(b), nil
}
