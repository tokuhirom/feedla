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

	// CookieSecure controls the session cookie's Secure attribute (and, in
	// turn, whether the __Host- name prefix is used): "auto" (default)
	// sets Secure only when the request itself arrived over TLS (r.TLS !=
	// nil); it deliberately does not trust X-Forwarded-Proto, since that
	// requires the operator to explicitly say the proxy is trustworthy.
	// Behind a TLS-terminating reverse proxy, set this to "true".
	CookieSecure string
	// PublicOrigin, if set, is the Origin the CSRF check requires for
	// cookie-authenticated state-changing requests, overriding the
	// request's own Host header. Needed behind a reverse proxy that
	// rewrites Host.
	PublicOrigin string
	// MetricsToken, if set, allows GET /metrics to authenticate via
	// `Authorization: Bearer <token>` instead of a session, for monitoring
	// systems that can't hold a browser session.
	MetricsToken string
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
		CookieSecure:     getEnv("FR_COOKIE_SECURE", "auto"),
		PublicOrigin:     getEnv("FR_PUBLIC_ORIGIN", ""),
		MetricsToken:     getEnv("FR_METRICS_TOKEN", ""),
		FetchConcurrency: 32,
		RetentionDays:    30,
		RetentionPerFeed: 1000,
	}

	switch cfg.CookieSecure {
	case "auto", "true", "false":
	default:
		return Config{}, fmt.Errorf("config: FR_COOKIE_SECURE: invalid value %q (want auto/true/false)", cfg.CookieSecure)
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
