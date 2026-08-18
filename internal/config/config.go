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
	BackupRemote     BackupRemote
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

	// Quota holds the per-user resource limits described in
	// docs/multi-user-design.md's "リソース制限・abuse 対策" section.
	Quota Quota
}

// BackupRemote holds the FR_BACKUP_REMOTE_* settings for mirroring daily
// local backups (per BackupDir) into an S3-compatible object storage
// bucket, such as Sakura Cloud Object Storage. Endpoint == "" disables it;
// it requires BackupDir to also be set, since it uploads that daily
// snapshot rather than producing its own.
type BackupRemote struct {
	Endpoint    string // FR_BACKUP_REMOTE_ENDPOINT, e.g. https://s3.isk01.sakurastorage.jp
	Region      string // FR_BACKUP_REMOTE_REGION
	Bucket      string // FR_BACKUP_REMOTE_BUCKET
	AccessKey   string // FR_BACKUP_REMOTE_ACCESS_KEY
	SecretKey   string // FR_BACKUP_REMOTE_SECRET_KEY
	Prefix      string // FR_BACKUP_REMOTE_PREFIX
	Generations int    // FR_BACKUP_REMOTE_GENERATIONS
}

// Quota holds the FR_QUOTA_* limits. All limits are per user (per
// created_by for scrape sources); rate limits use a fixed one-hour or
// one-minute window.
type Quota struct {
	MaxSubscriptions int // FR_QUOTA_MAX_SUBSCRIPTIONS
	MaxScrapeSources int // FR_QUOTA_MAX_SCRAPE_SOURCES
	MaxPins          int // FR_QUOTA_MAX_PINS
	MaxIgnoreWords   int // FR_QUOTA_MAX_IGNORE_WORDS
	OPMLMaxFeeds     int // FR_QUOTA_OPML_MAX_FEEDS

	FeedAddPerHour int // FR_QUOTA_FEED_ADD_PER_HOUR
	RefreshPerHour int // FR_QUOTA_REFRESH_PER_HOUR
	PreviewPerHour int // FR_QUOTA_PREVIEW_PER_HOUR
	APIPerMinute   int // FR_QUOTA_API_PER_MINUTE
}

// Load reads configuration from the environment, applying the defaults
// documented in README.md for anything unset.
func Load() (Config, error) {
	cfg := Config{
		Listen:    getEnv("FR_LISTEN", "127.0.0.1:8080"),
		DBPath:    getEnv("FR_DB_PATH", "feedla.db"),
		UserAgent: getEnv("FR_USER_AGENT", "feedla/0.1"),
		LogLevel:  getEnv("FR_LOG_LEVEL", "info"),
		BackupDir: getEnv("FR_BACKUP_DIR", ""),
		BackupRemote: BackupRemote{
			Endpoint:    getEnv("FR_BACKUP_REMOTE_ENDPOINT", ""),
			Region:      getEnv("FR_BACKUP_REMOTE_REGION", "jp-north-1"),
			Bucket:      getEnv("FR_BACKUP_REMOTE_BUCKET", ""),
			AccessKey:   getEnv("FR_BACKUP_REMOTE_ACCESS_KEY", ""),
			SecretKey:   getEnv("FR_BACKUP_REMOTE_SECRET_KEY", ""),
			Prefix:      getEnv("FR_BACKUP_REMOTE_PREFIX", "feedla/"),
			Generations: 5,
		},
		CookieSecure:     getEnv("FR_COOKIE_SECURE", "auto"),
		PublicOrigin:     getEnv("FR_PUBLIC_ORIGIN", ""),
		MetricsToken:     getEnv("FR_METRICS_TOKEN", ""),
		FetchConcurrency: 32,
		RetentionDays:    30,
		RetentionPerFeed: 1000,
		Quota: Quota{
			MaxSubscriptions: 2000,
			MaxScrapeSources: 50,
			MaxPins:          10000,
			MaxIgnoreWords:   1000,
			OPMLMaxFeeds:     2000,
			FeedAddPerHour:   60,
			RefreshPerHour:   30,
			PreviewPerHour:   30,
			APIPerMinute:     600,
		},
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
	if cfg.BackupRemote.Generations, err = getEnvInt("FR_BACKUP_REMOTE_GENERATIONS", cfg.BackupRemote.Generations); err != nil {
		return Config{}, err
	}
	if cfg.BackupRemote.Endpoint != "" {
		if cfg.BackupDir == "" {
			return Config{}, fmt.Errorf("config: FR_BACKUP_REMOTE_ENDPOINT requires FR_BACKUP_DIR to also be set (it uploads the daily local backup)")
		}
		if cfg.BackupRemote.Bucket == "" || cfg.BackupRemote.AccessKey == "" || cfg.BackupRemote.SecretKey == "" {
			return Config{}, fmt.Errorf("config: FR_BACKUP_REMOTE_ENDPOINT requires FR_BACKUP_REMOTE_BUCKET, FR_BACKUP_REMOTE_ACCESS_KEY, and FR_BACKUP_REMOTE_SECRET_KEY to also be set")
		}
	}
	if cfg.FetchMinInterval, err = getEnvDuration("FR_FETCH_MIN_INTERVAL", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.FetchMaxInterval, err = getEnvDuration("FR_FETCH_MAX_INTERVAL", 12*time.Hour); err != nil {
		return Config{}, err
	}

	if cfg.Quota.MaxSubscriptions, err = getEnvInt("FR_QUOTA_MAX_SUBSCRIPTIONS", cfg.Quota.MaxSubscriptions); err != nil {
		return Config{}, err
	}
	if cfg.Quota.MaxScrapeSources, err = getEnvInt("FR_QUOTA_MAX_SCRAPE_SOURCES", cfg.Quota.MaxScrapeSources); err != nil {
		return Config{}, err
	}
	if cfg.Quota.MaxPins, err = getEnvInt("FR_QUOTA_MAX_PINS", cfg.Quota.MaxPins); err != nil {
		return Config{}, err
	}
	if cfg.Quota.MaxIgnoreWords, err = getEnvInt("FR_QUOTA_MAX_IGNORE_WORDS", cfg.Quota.MaxIgnoreWords); err != nil {
		return Config{}, err
	}
	if cfg.Quota.OPMLMaxFeeds, err = getEnvInt("FR_QUOTA_OPML_MAX_FEEDS", cfg.Quota.OPMLMaxFeeds); err != nil {
		return Config{}, err
	}
	if cfg.Quota.FeedAddPerHour, err = getEnvInt("FR_QUOTA_FEED_ADD_PER_HOUR", cfg.Quota.FeedAddPerHour); err != nil {
		return Config{}, err
	}
	if cfg.Quota.RefreshPerHour, err = getEnvInt("FR_QUOTA_REFRESH_PER_HOUR", cfg.Quota.RefreshPerHour); err != nil {
		return Config{}, err
	}
	if cfg.Quota.PreviewPerHour, err = getEnvInt("FR_QUOTA_PREVIEW_PER_HOUR", cfg.Quota.PreviewPerHour); err != nil {
		return Config{}, err
	}
	if cfg.Quota.APIPerMinute, err = getEnvInt("FR_QUOTA_API_PER_MINUTE", cfg.Quota.APIPerMinute); err != nil {
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
