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
//
// It also clears every other variable Load reads. `make test` exports the
// developer's .env, so a test asserting a default would otherwise be
// asserting whatever that developer happens to have configured — passing on
// a fresh clone and failing on a working one, or the reverse. t.Setenv
// restores all of it when the test ends.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:55432/flowlens?sslmode=disable")

	for _, key := range []string{
		"APP_ENV",
		"API_PORT",
		"WEB_BASE_URL",
		"SESSION_TTL_HOURS",
		"APP_PUBLIC_URL",
		"RUN_MIGRATIONS",
		"ALLOW_SIGNUP",
		"METRICS_TOKEN",
		"TRUSTED_PROXY_HOPS",
		"SYNC_WORKER_ENABLED",
		"SYNC_WORKER_POLL_INTERVAL",
		"GITLAB_CA_CERT_FILE",
		"GITLAB_TLS_INSECURE_SKIP_VERIFY",
	} {
		t.Setenv(key, "")
	}
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

			cfg, err := Load("test")

			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoad_DecodesValidEncryptionKey(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())

	cfg, err := Load("test")

	require.NoError(t, err)
	assert.Equal(t, []byte(strings.Repeat("k", 32)), cfg.EncryptionKey)
}

func TestLoad_SyncWorkerAndAppPublicURLDefaults(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())

	cfg, err := Load("test")

	require.NoError(t, err)
	assert.Empty(t, cfg.AppPublicURL)
	assert.True(t, cfg.SyncWorkerEnabled)
	assert.Equal(t, 5*time.Second, cfg.SyncWorkerPollInterval)
}

// The self-hosting defaults: a container you start should come up with a
// working schema and a way to create the first account, and should not
// trust a proxy header nobody told it about.
func TestLoad_SelfHostingDefaults(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())

	cfg, err := Load("v1.2.3")

	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", cfg.Version)
	assert.True(t, cfg.RunMigrations)
	assert.True(t, cfg.AllowSignup)
	assert.Empty(t, cfg.MetricsToken)
	assert.Zero(t, cfg.TrustedProxyHops)
}

func TestLoad_RejectsNegativeTrustedProxyHops(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())
	t.Setenv("TRUSTED_PROXY_HOPS", "-1")

	cfg, err := Load("test")

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "TRUSTED_PROXY_HOPS")
}

// The GitLab TLS defaults are deliberately unlike the rest: skip-verify is
// on out of the box because FlowLens targets self-hosted GitLab CE behind a
// private CA. Naming a CA is how an operator switches verification back on.
func TestLoad_GitlabTLSDefaultsToSkipVerify(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())

	cfg, err := Load("test")

	require.NoError(t, err)
	assert.True(t, cfg.GitlabTLSInsecureSkipVerify)
	assert.Empty(t, cfg.GitlabCACertFile)
}

func TestLoad_GitlabTLSOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())
	t.Setenv("GITLAB_TLS_INSECURE_SKIP_VERIFY", "false")
	t.Setenv("GITLAB_CA_CERT_FILE", "/etc/ssl/certs/corp-ca.pem")

	cfg, err := Load("test")

	require.NoError(t, err)
	assert.False(t, cfg.GitlabTLSInsecureSkipVerify)
	assert.Equal(t, "/etc/ssl/certs/corp-ca.pem", cfg.GitlabCACertFile)
}

func TestLoad_RejectsInvalidGitlabTLSFlag(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())
	t.Setenv("GITLAB_TLS_INSECURE_SKIP_VERIFY", "sometimes")

	cfg, err := Load("test")

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "GITLAB_TLS_INSECURE_SKIP_VERIFY")
}

func TestLoad_SyncWorkerAndAppPublicURLOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ENCRYPTION_KEY", validEncryptionKey())
	t.Setenv("APP_PUBLIC_URL", "https://flowlens.example.com")
	t.Setenv("SYNC_WORKER_ENABLED", "false")
	t.Setenv("SYNC_WORKER_POLL_INTERVAL", "10s")

	cfg, err := Load("test")

	require.NoError(t, err)
	assert.Equal(t, "https://flowlens.example.com", cfg.AppPublicURL)
	assert.False(t, cfg.SyncWorkerEnabled)
	assert.Equal(t, 10*time.Second, cfg.SyncWorkerPollInterval)
}
