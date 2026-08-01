package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validKey() []byte {
	return []byte("01234567890123456789012345678901") // 32 bytes
}

func TestNew_ValidatesKeyLength(t *testing.T) {
	tests := []struct {
		name    string
		key     []byte
		wantErr bool
	}{
		{"32 bytes", validKey(), false},
		{"too short", []byte("short"), true},
		{"too long", append(validKey(), 'x'), true},
		{"empty", []byte{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, c)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, c)
		})
	}
}

func TestCipher_EncryptDecrypt_RoundTrips(t *testing.T) {
	c, err := New(validKey())
	require.NoError(t, err)

	tests := []struct {
		name      string
		plaintext string
	}{
		{"typical token", "glpat-abcdefghijklmnopqrst"},
		{"empty string", ""},
		{"unicode", "héllo wörld 🌍"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := c.Encrypt(tt.plaintext)
			require.NoError(t, err)

			got, err := c.Decrypt(ciphertext)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, got)
		})
	}
}

func TestCipher_Encrypt_NonceIsRandomPerCall(t *testing.T) {
	c, err := New(validKey())
	require.NoError(t, err)

	first, err := c.Encrypt("same plaintext")
	require.NoError(t, err)
	second, err := c.Encrypt("same plaintext")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestCipher_Decrypt_DetectsTampering(t *testing.T) {
	c, err := New(validKey())
	require.NoError(t, err)

	ciphertext, err := c.Encrypt("sensitive-token")
	require.NoError(t, err)

	tests := []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"flipped byte", func(ct []byte) []byte {
			tampered := append([]byte(nil), ct...)
			tampered[len(tampered)-1] ^= 0xFF
			return tampered
		}},
		{"truncated", func(ct []byte) []byte {
			return ct[:len(ct)-1]
		}},
		{"too short for nonce", func([]byte) []byte {
			return []byte("short")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Decrypt(tt.corrupt(ciphertext))
			assert.Error(t, err)
		})
	}
}

func TestCipher_Decrypt_FailsWithWrongKey(t *testing.T) {
	c1, err := New(validKey())
	require.NoError(t, err)
	c2, err := New([]byte("98765432109876543210987654321098"))
	require.NoError(t, err)

	ciphertext, err := c1.Encrypt("sensitive-token")
	require.NoError(t, err)

	_, err = c2.Decrypt(ciphertext)
	assert.Error(t, err)
}
