// Package config loads and validates runtime configuration from the
// environment. In development it can hydrate from a local .env file; in
// production values are expected to come from the container environment.
// Secrets are never hard-coded.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/joho/godotenv"
)

// Config holds all runtime settings for the API.
type Config struct {
	// Version is the build version stamped into the binary at link time
	// (see the Makefile's -ldflags). It is reported by GET /version so a
	// self-hosted operator can tell which release is running.
	Version string

	Env        string // "development" or "production"
	Port       string
	WebBaseURL string

	DatabaseURL string

	// RunMigrations applies the embedded schema migrations at startup.
	// It defaults to true: a self-hosted deployment is meant to be a
	// container you start, with no separate migrate step. Set it to false
	// where migrations are run as their own deploy stage.
	RunMigrations bool

	// AllowSignup gates POST /auth/signup. It defaults to true, but a
	// self-hosted instance reachable from a network you do not control
	// should turn it off once its accounts exist, so that reaching the
	// login page is not enough to create one. The very first account is
	// always allowed regardless, otherwise a fresh instance started with
	// signup off could never be bootstrapped.
	AllowSignup bool

	// TrustedProxyHops is how many reverse proxies sit in front of the API
	// and can be trusted to have appended to X-Forwarded-For. It defaults
	// to 0 — trust nothing, key rate limits on the socket address. The
	// bundled compose file sets 1 for the Next.js proxy in front, and 2
	// when a TLS terminator is added ahead of that.
	TrustedProxyHops int

	// MetricsToken, when set, requires GET /metrics to present it as a
	// bearer token. Empty (the default) leaves the endpoint open, which is
	// what the bundled compose file relies on: it never publishes the API
	// port, so /metrics is only reachable from inside the Docker network.
	MetricsToken string

	SessionTTL time.Duration

	// EncryptionKey is the decoded 32-byte AES-256 key used to encrypt
	// secrets at rest (GitLab access tokens, webhook secrets).
	EncryptionKey []byte

	// AppPublicURL is the URL GitLab must be able to reach to deliver
	// webhooks. When empty, webhook auto-registration is skipped.
	AppPublicURL string

	SyncWorkerEnabled      bool
	SyncWorkerPollInterval time.Duration

	// GitlabCACertFile is the path to a PEM bundle trusted when calling the
	// GitLab API, for a self-hosted instance behind a private CA. When set
	// it takes precedence over GitlabTLSInsecureSkipVerify.
	GitlabCACertFile string

	// GitlabTLSInsecureSkipVerify disables TLS certificate verification for
	// outbound GitLab API calls. It defaults to **true**: FlowLens targets
	// self-hosted GitLab CE, where the instance certificate is typically
	// signed by a private CA the API host does not trust, and a failed
	// handshake there surfaces only as an unexplained "unreachable"
	// connection. Set it to false (or name a CA above) once certificates
	// are verifiable. It applies to GitLab only — a future GitHub client
	// gets its own policy and stays verified.
	GitlabTLSInsecureSkipVerify bool
}

// IsProduction reports whether the API runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from the environment. When a .env file exists
// it is loaded first (existing environment variables take precedence).
// version is the build stamp the caller was linked with.
func Load(version string) (*Config, error) {
	// Best-effort: a missing .env is fine (e.g. in Docker / production).
	_ = godotenv.Load()

	cfg := &Config{
		Version:     version,
		Env:         getEnv("APP_ENV", "development"),
		Port:        getEnv("API_PORT", "8080"),
		WebBaseURL:  getEnv("WEB_BASE_URL", "http://localhost:4000"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}

	runMigrations, err := strconv.ParseBool(getEnv("RUN_MIGRATIONS", "true"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid RUN_MIGRATIONS: %w", err)
	}
	cfg.RunMigrations = runMigrations

	allowSignup, err := strconv.ParseBool(getEnv("ALLOW_SIGNUP", "true"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid ALLOW_SIGNUP: %w", err)
	}
	cfg.AllowSignup = allowSignup

	cfg.MetricsToken = os.Getenv("METRICS_TOKEN")

	trustedProxyHops, err := strconv.Atoi(getEnv("TRUSTED_PROXY_HOPS", "0"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid TRUSTED_PROXY_HOPS: %w", err)
	}
	if trustedProxyHops < 0 {
		return nil, fmt.Errorf("config: TRUSTED_PROXY_HOPS must not be negative, got %d", trustedProxyHops)
	}
	cfg.TrustedProxyHops = trustedProxyHops

	ttlHours, err := strconv.Atoi(getEnv("SESSION_TTL_HOURS", "168"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid SESSION_TTL_HOURS: %w", err)
	}
	cfg.SessionTTL = time.Duration(ttlHours) * time.Hour

	encryptionKey, err := decodeEncryptionKey(os.Getenv("ENCRYPTION_KEY"))
	if err != nil {
		return nil, err
	}
	cfg.EncryptionKey = encryptionKey

	cfg.AppPublicURL = os.Getenv("APP_PUBLIC_URL")

	syncWorkerEnabled, err := strconv.ParseBool(getEnv("SYNC_WORKER_ENABLED", "true"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid SYNC_WORKER_ENABLED: %w", err)
	}
	cfg.SyncWorkerEnabled = syncWorkerEnabled

	pollInterval, err := time.ParseDuration(getEnv("SYNC_WORKER_POLL_INTERVAL", "5s"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid SYNC_WORKER_POLL_INTERVAL: %w", err)
	}
	cfg.SyncWorkerPollInterval = pollInterval

	cfg.GitlabCACertFile = os.Getenv("GITLAB_CA_CERT_FILE")

	skipVerify, err := strconv.ParseBool(getEnv("GITLAB_TLS_INSECURE_SKIP_VERIFY", "true"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid GITLAB_TLS_INSECURE_SKIP_VERIFY: %w", err)
	}
	cfg.GitlabTLSInsecureSkipVerify = skipVerify

	return cfg, nil
}

// decodeEncryptionKey base64-decodes ENCRYPTION_KEY and validates it is a
// usable AES-256 key. It is required; a missing or malformed value is a
// startup error.
func decodeEncryptionKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("config: ENCRYPTION_KEY is required")
	}

	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("config: ENCRYPTION_KEY must be valid base64: %w", err)
	}

	if len(key) != crypto.KeySize {
		return nil, fmt.Errorf("config: ENCRYPTION_KEY must decode to %d bytes, got %d", crypto.KeySize, len(key))
	}

	return key, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
