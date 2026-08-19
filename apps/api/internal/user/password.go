package user

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// ErrPasswordTooShort is returned by SignUp when the password does not meet
// the minimum length requirement.
var ErrPasswordTooShort = errors.New("user: password must be at least 8 characters")

// hashPassword hashes a plaintext password for storage. It lives in the user
// package rather than in auth so that auth (sessions) can depend on user
// without creating an import cycle.
func hashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("user: hash password: %w", err)
	}
	return string(hash), nil
}

// HashPassword is hashPassword exported for the `flowlens-api hash-password`
// subcommand (issue #210), which prints a hash an operator can paste into
// users.password_hash to recover an account whose password was lost. There
// is no password-reset email flow to do it for them — FlowLens has no mail
// transport and is aimed at closed networks — so this is the documented
// recovery path in docs/self-hosting.md.
func HashPassword(password string) (string, error) {
	return hashPassword(password)
}

// verifyPassword reports whether password matches the stored hash.
func verifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
