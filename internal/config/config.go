// Package config loads feedla's runtime configuration from environment
// variables, per the FR_* variables documented in README.md.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds every FR_* environment variable feedla understands.
type Config struct {
	Listen           string
	DBPath           string
	FetchConcurrency int
	FetchMinInterval time.Duration
	FetchMaxInterval time.Duration
	RetentionDays    int
	RetentionPerFeed int
	BackupDir        string
	UserAgent        string
	LogLevel         string
}

// Load reads configuration from the environment, applying the defaults
// documented in README.md for anything unset.
func Load() (Config, error) {
	cfg := Config{
		Listen:           getEnv("FR_LISTEN", "127.0.0.1:8080"),
		DBPath:           getEnv("FR_DB_PATH", "feedla.db"),
		UserAgent:        getEnv("FR_USER_AGENT", "feedla/0.1"),
		LogLevel:         getEnv("FR_LOG_LEVEL", "info"),
		BackupDir:        getEnv("FR_BACKUP_DIR", ""),
		FetchConcurrency: 32,
		RetentionDays:    30,
		RetentionPerFeed: 1000,
	}

	var err error
	if cfg.FetchConcurrency, err = getEnvInt("FR_FETCH_CONCURRENCY", cfg.FetchConcurrency); err != nil {
		return Config{}, err
	}
	if cfg.RetentionDays, err = getEnvInt("FR_RETENTION_DAYS", cfg.RetentionDays); err != nil {
		return Config{}, err
	}
	if cfg.RetentionPerFeed, err = getEnvInt("FR_RETENTION_PER_FEED", cfg.RetentionPerFeed); err != nil {
		return Config{}, err
	}
	if cfg.FetchMinInterval, err = getEnvDuration("FR_FETCH_MIN_INTERVAL", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.FetchMaxInterval, err = getEnvDuration("FR_FETCH_MAX_INTERVAL", 12*time.Hour); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s: invalid int %q: %w", key, v, err)
	}
	return n, nil
}

func getEnvDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s: invalid duration %q: %w", key, v, err)
	}
	return d, nil
}
