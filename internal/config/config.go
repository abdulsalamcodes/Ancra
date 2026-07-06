package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	Port                 string
	DatabaseURL          string
	NombaClientID        string
	NombaClientSecret    string
	NombaAccountID       string
	NombaSubAccountID    string
	NombaBaseURL         string
	NombaWebhookSecret   string
	APIKey               string // legacy static key; optional when DB keys are used
	AdminSecret          string // protects /admin/* endpoints
	SweepIntervalSeconds int
}

// Load reads the .env file (if present) and then reads environment variables.
// Environment variables already set in the shell take precedence over the file.
func Load() (*Config, error) {
	// Best-effort load; if .env is absent in production that is fine.
	_ = godotenv.Load()

	cfg := &Config{}

	cfg.Port = getEnv("PORT", "8080")
	cfg.DatabaseURL = mustGetEnv("DATABASE_URL")
	cfg.NombaClientID = mustGetEnv("NOMBA_CLIENT_ID")
	cfg.NombaClientSecret = mustGetEnv("NOMBA_CLIENT_SECRET")
	cfg.NombaAccountID = mustGetEnv("NOMBA_ACCOUNT_ID")
	cfg.NombaSubAccountID = mustGetEnv("NOMBA_SUB_ACCOUNT_ID")
	cfg.NombaBaseURL = getEnv("NOMBA_BASE_URL", "https://api.nomba.com/v1")
	// Support both NOMBA_WEBHOOK_SECRET and NOMBA_WEBHOOK_SIGNING_KEY (hackathon alias).
	cfg.NombaWebhookSecret = getEnv("NOMBA_WEBHOOK_SECRET", "")
	if cfg.NombaWebhookSecret == "" {
		cfg.NombaWebhookSecret = mustGetEnv("NOMBA_WEBHOOK_SIGNING_KEY")
	}
	cfg.APIKey = getEnv("API_KEY", "")     // optional; legacy static key
	cfg.AdminSecret = mustGetEnv("ADMIN_SECRET")

	sweepStr := getEnv("SWEEP_INTERVAL_SECONDS", "60")
	sweep, err := strconv.Atoi(sweepStr)
	if err != nil {
		return nil, fmt.Errorf("config: SWEEP_INTERVAL_SECONDS must be an integer, got %q", sweepStr)
	}
	cfg.SweepIntervalSeconds = sweep

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("config: required environment variable %q is not set", key))
	}
	return v
}
