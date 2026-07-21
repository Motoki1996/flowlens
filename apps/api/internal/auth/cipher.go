package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// TokenCipher encrypts and decrypts sensitive strings (GitHub access
// tokens). It is an interface so the local AES-GCM implementation can be
// swapped for an Azure Key Vault backed implementation without touching
// callers.
type TokenCipher interface {
	Encrypt(plaintext string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

// AESGCMCipher is the local development implementation of TokenCipher. It
// uses AES-256-GCM with a random nonce prepended to each ciphertext.
type AESGCMCipher struct {
	gcm cipher.AEAD
}

// NewAESGCMCipher builds a cipher from a 32-byte key.
func NewAESGCMCipher(key []byte) (*AESGCMCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("auth: AES key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: new gcm: %w", err)
	}
	return &AESGCMCipher{gcm: gcm}, nil
}

// Encrypt returns nonce || ciphertext.
func (c *AESGCMCipher) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("auth: read nonce: %w", err)
	}
	return c.gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt.
func (c *AESGCMCipher) Decrypt(ciphertext []byte) (string, error) {
	nonceSize := c.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("auth: ciphertext too short")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := c.gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("auth: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// DecodeKey is a small helper for callers/tests that hold a base64 key.
func DecodeKey(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}
