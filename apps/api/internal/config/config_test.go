package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEncryptionKey() string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
}

// setBaseEnv sets the environment variables Load requires besides
// ENCRYPTION_KEY, so each test only has to vary what it's testing.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:55432/flowlens?sslmode=disable")
}

func TestLoad_RequiresEncryptionKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{"missing", "", "ENCRYPTION_KEY is required"},
		{"not base64", "not-valid-base64!!!", "must be valid base64"},
		{"wrong decoded length", base64.StdEncoding.EncodeToString([]byte("too-short")), "must decode to 32 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv("ENCRYPTION_KEY", tt.key)

			cfg, err := Load()

			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoad_DecodesValidEncryptionKey(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, []byte(strings.Repeat("k", 32)), cfg.EncryptionKey)
}

func TestLoad_SyncWorkerAndAppPublicURLDefaults(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())

	cfg, err := Load()

	require.NoError(t, err)
	assert.Empty(t, cfg.AppPublicURL)
	assert.True(t, cfg.SyncWorkerEnabled)
	assert.Equal(t, 5*time.Second, cfg.SyncWorkerPollInterval)
}

func TestLoad_SyncWorkerAndAppPublicURLOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())
	t.Setenv("APP_PUBLIC_URL", "https://flowlens.example.com")
	t.Setenv("SYNC_WORKER_ENABLED", "false")
	t.Setenv("SYNC_WORKER_POLL_INTERVAL", "10s")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, "https://flowlens.example.com", cfg.AppPublicURL)
	assert.False(t, cfg.SyncWorkerEnabled)
	assert.Equal(t, 10*time.Second, cfg.SyncWorkerPollInterval)
}
