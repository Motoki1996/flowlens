package main

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestHashPassword covers the recovery path documented in
// docs/self-hosting.md: whatever this prints has to be a hash the running
// API will accept, since an operator pastes it straight into
// users.password_hash.
func TestHashPassword(t *testing.T) {
	var out bytes.Buffer
	if err := hashPassword(strings.NewReader("correct-horse\n"), &out); err != nil {
		t.Fatalf("hashPassword: %v", err)
	}

	hash := strings.TrimSpace(out.String())
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("correct-horse")); err != nil {
		t.Fatalf("printed hash does not verify against the password: %v", err)
	}
	if strings.Contains(hash, "correct-horse") {
		t.Fatal("the plaintext password must not appear in the output")
	}
}

func TestHashPassword_RejectsEmptyInput(t *testing.T) {
	var out bytes.Buffer
	if err := hashPassword(strings.NewReader(""), &out); err == nil {
		t.Fatal("expected an error when stdin carries no password")
	}
}

// TestHashPassword_RejectsShortPassword pins that the subcommand enforces
// the same minimum length as signup, so recovery cannot quietly install a
// weaker password than the API would ever accept.
func TestHashPassword_RejectsShortPassword(t *testing.T) {
	var out bytes.Buffer
	if err := hashPassword(strings.NewReader("short\n"), &out); err == nil {
		t.Fatal("expected an error for a password below the minimum length")
	}
}
