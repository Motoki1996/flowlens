// Package config loads and validates runtime configuration from the
// environment. In development it can hydrate from a local .env file; in
// production values are expected to come from the container environment
// (and, later, Azure Key Vault). Secrets are never hard-coded.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime settings for the API.
type Config struct {
	Env        string // "development" or "production"
	Port       string
	WebBaseURL string
	APIBaseURL string

	DatabaseURL string

	GitHubClientID     string
	GitHubClientSecret string

	// TokenEncryptionKey is a decoded 32-byte AES key.
	TokenEncryptionKey []byte

	SessionTTL time.Duration
}

// IsProduction reports whether the API runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from the environment. When a .env file exists
// it is loaded first (existing environment variables take precedence).
func Load() (*Config, error) {
	// Best-effort: a missing .env is fine (e.g. in Docker / production).
	_ = godotenv.Load()

	cfg := &Config{
		Env:                getEnv("APP_ENV", "development"),
		Port:               getEnv("API_PORT", "8080"),
		WebBaseURL:         getEnv("WEB_BASE_URL", "http://localhost:3000"),
		APIBaseURL:         getEnv("API_BASE_URL", "http://localhost:8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		GitHubClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}

	ttlHours, err := strconv.Atoi(getEnv("SESSION_TTL_HOURS", "168"))
	if err != nil {
		return nil, fmt.Errorf("config: invalid SESSION_TTL_HOURS: %w", err)
	}
	cfg.SessionTTL = time.Duration(ttlHours) * time.Hour

	key, err := decodeEncryptionKey(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	if err != nil {
		return nil, err
	}
	cfg.TokenEncryptionKey = key

	return cfg, nil
}

// decodeEncryptionKey decodes a base64 std-encoded 32-byte AES key.
func decodeEncryptionKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("config: TOKEN_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("config: TOKEN_ENCRYPTION_KEY must be base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("config: TOKEN_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
