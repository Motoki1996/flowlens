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
	Env        string // "development" or "production"
	Port       string
	WebBaseURL string

	DatabaseURL string

	SessionTTL time.Duration

	// EncryptionKey is the decoded 32-byte AES-256 key used to encrypt
	// secrets at rest (GitLab access tokens, webhook secrets).
	EncryptionKey []byte

	// AppPublicURL is the URL GitLab must be able to reach to deliver
	// webhooks. When empty, webhook auto-registration is skipped.
	AppPublicURL string

	SyncWorkerEnabled      bool
	SyncWorkerPollInterval time.Duration
}

// IsProduction reports whether the API runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from the environment. When a .env file exists
// it is loaded first (existing environment variables take precedence).
func Load() (*Config, error) {
	// Best-effort: a missing .env is fine (e.g. in Docker / production).
	_ = godotenv.Load()

	cfg := &Config{
		Env:         getEnv("APP_ENV", "development"),
		Port:        getEnv("API_PORT", "8080"),
		WebBaseURL:  getEnv("WEB_BASE_URL", "http://localhost:3000"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}

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
