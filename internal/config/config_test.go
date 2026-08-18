package config

import "testing"

func TestLoadCookieSecureDefaultAndValidation(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CookieSecure != "auto" {
		t.Fatalf("CookieSecure = %q, want auto by default", cfg.CookieSecure)
	}

	t.Setenv("FR_COOKIE_SECURE", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CookieSecure != "true" {
		t.Fatalf("CookieSecure = %q, want true", cfg.CookieSecure)
	}

	t.Setenv("FR_COOKIE_SECURE", "nonsense")
	if _, err := Load(); err == nil {
		t.Fatal("Load: want error for invalid FR_COOKIE_SECURE")
	}
}

func TestLoadQuotaDefaultsAndOverride(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Quota{
		MaxSubscriptions: 2000,
		MaxScrapeSources: 50,
		MaxPins:          10000,
		MaxIgnoreWords:   1000,
		OPMLMaxFeeds:     2000,
		FeedAddPerHour:   60,
		RefreshPerHour:   30,
		PreviewPerHour:   30,
		APIPerMinute:     600,
	}
	if cfg.Quota != want {
		t.Fatalf("default Quota = %+v, want %+v", cfg.Quota, want)
	}

	t.Setenv("FR_QUOTA_MAX_SUBSCRIPTIONS", "5")
	t.Setenv("FR_QUOTA_API_PER_MINUTE", "10")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Quota.MaxSubscriptions != 5 {
		t.Errorf("MaxSubscriptions = %d, want 5", cfg.Quota.MaxSubscriptions)
	}
	if cfg.Quota.APIPerMinute != 10 {
		t.Errorf("APIPerMinute = %d, want 10", cfg.Quota.APIPerMinute)
	}
	// Unrelated quota fields stay at their defaults.
	if cfg.Quota.MaxPins != 10000 {
		t.Errorf("MaxPins = %d, want unchanged default 10000", cfg.Quota.MaxPins)
	}

	t.Setenv("FR_QUOTA_MAX_SUBSCRIPTIONS", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("Load: want error for invalid FR_QUOTA_MAX_SUBSCRIPTIONS")
	}
}

func TestLoadBackupRemoteDefaultsDisabledAndValidation(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BackupRemote.Endpoint != "" {
		t.Fatalf("BackupRemote.Endpoint = %q, want empty (disabled) by default", cfg.BackupRemote.Endpoint)
	}
	if cfg.BackupRemote.Generations != 5 {
		t.Fatalf("BackupRemote.Generations = %d, want 5 by default", cfg.BackupRemote.Generations)
	}

	// Setting the endpoint without FR_BACKUP_DIR is rejected -- remote
	// backup uploads the daily local snapshot, so it can't run without one.
	t.Setenv("FR_BACKUP_REMOTE_ENDPOINT", "https://s3.isk01.sakurastorage.jp")
	t.Setenv("FR_BACKUP_REMOTE_BUCKET", "my-bucket")
	t.Setenv("FR_BACKUP_REMOTE_ACCESS_KEY", "ak")
	t.Setenv("FR_BACKUP_REMOTE_SECRET_KEY", "sk")
	if _, err := Load(); err == nil {
		t.Fatal("Load: want error for FR_BACKUP_REMOTE_ENDPOINT without FR_BACKUP_DIR")
	}

	t.Setenv("FR_BACKUP_DIR", "/var/lib/feedla/backup")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BackupRemote.Bucket != "my-bucket" || cfg.BackupRemote.AccessKey != "ak" || cfg.BackupRemote.SecretKey != "sk" {
		t.Fatalf("BackupRemote = %+v, want bucket/access/secret populated from env", cfg.BackupRemote)
	}
	if cfg.BackupRemote.Region != "jp-north-1" {
		t.Fatalf("BackupRemote.Region = %q, want default jp-north-1", cfg.BackupRemote.Region)
	}
	if cfg.BackupRemote.Prefix != "feedla/" {
		t.Fatalf("BackupRemote.Prefix = %q, want default feedla/", cfg.BackupRemote.Prefix)
	}

	// Endpoint set but missing bucket/keys is rejected too.
	t.Setenv("FR_BACKUP_REMOTE_BUCKET", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load: want error for FR_BACKUP_REMOTE_ENDPOINT without FR_BACKUP_REMOTE_BUCKET")
	}
}
