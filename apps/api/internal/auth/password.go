package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// ErrPasswordTooShort is returned by HashPassword when the password does
// not meet the minimum length requirement.
var ErrPasswordTooShort = errors.New("auth: password must be at least 8 characters")

// HashPassword hashes a plaintext password for storage.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches the stored hash.
func VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
