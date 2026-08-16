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
