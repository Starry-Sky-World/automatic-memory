package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port          string
	LogLevel      string
	DatabaseURL   string
	AuthToken     string
	MigrationsDir string
}

func Load() Config {
	cfg := Config{
		Port:          envOrDefault("CLOUD_SYNC_PORT", "8090"),
		LogLevel:      envOrDefault("CLOUD_SYNC_LOG_LEVEL", "info"),
		DatabaseURL:   envOrDefault("CLOUD_SYNC_DATABASE_URL", "file:cloudsync.db"),
		AuthToken:     strings.TrimSpace(os.Getenv("CLOUD_SYNC_AUTH_TOKEN")),
		MigrationsDir: envOrDefault("CLOUD_SYNC_MIGRATIONS_DIR", "migrations"),
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		cfg.Port = p
	}
	return cfg
}

func envOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func IntOrDefault(v string, fallback int) int {
	if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i > 0 {
		return i
	}
	return fallback
}
