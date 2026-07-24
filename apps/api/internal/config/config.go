// Package config loads and validates runtime configuration from the
// environment. In development it can hydrate from a local .env file; in
// production values are expected to come from the container environment.
// Secrets are never hard-coded.
package config

import (
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

	DatabaseURL string

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

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
