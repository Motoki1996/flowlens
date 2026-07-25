// Package crypto provides authenticated encryption for secrets at rest
// (GitLab access tokens, webhook secrets) using AES-256-GCM.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// KeySize is the required key length in bytes for AES-256.
const KeySize = 32

// Cipher encrypts and decrypts strings with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 32-byte AES-256 key.
func New(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: key must be %d bytes, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext with AES-256-GCM, returning nonce||ciphertext.
// Each call draws a fresh random nonce, so encrypting the same plaintext
// twice yields different output.
func (c *Cipher) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	return c.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt. It returns an error if ciphertext is truncated
// or fails GCM authentication (tampered data or wrong key).
func (c *Cipher) Decrypt(ciphertext []byte) (string, error) {
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}

	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}

	return string(plaintext), nil
}
