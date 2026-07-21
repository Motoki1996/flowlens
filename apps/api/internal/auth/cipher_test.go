package auth_test

import (
	"testing"

	"github.com/flowlens/api/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestAESGCMCipher_RoundTrip(t *testing.T) {
	cipher, err := auth.NewAESGCMCipher(newTestKey())
	require.NoError(t, err)

	plaintext := "gho_secretaccesstoken"
	encrypted, err := cipher.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, string(encrypted), "ciphertext must not equal plaintext")

	decrypted, err := cipher.Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestAESGCMCipher_DifferentNoncePerCall(t *testing.T) {
	cipher, err := auth.NewAESGCMCipher(newTestKey())
	require.NoError(t, err)

	a, err := cipher.Encrypt("same")
	require.NoError(t, err)
	b, err := cipher.Encrypt("same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "encrypting the same value twice must differ (random nonce)")
}

func TestNewAESGCMCipher_RejectsBadKey(t *testing.T) {
	_, err := auth.NewAESGCMCipher([]byte("too-short"))
	assert.Error(t, err)
}

func TestAESGCMCipher_DecryptRejectsShortInput(t *testing.T) {
	cipher, err := auth.NewAESGCMCipher(newTestKey())
	require.NoError(t, err)
	_, err = cipher.Decrypt([]byte{0x01})
	assert.Error(t, err)
}
