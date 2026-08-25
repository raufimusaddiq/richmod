package config

import "testing"

func TestLoadAPIRejectsShortSessionSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "too-short")
	if _, err := LoadAPI(); err == nil {
		t.Fatal("LoadAPI accepted a short session secret")
	}
}

func TestLoadAPIAcceptsRequiredConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if cfg.Address != ":8080" {
		t.Fatalf("Address = %q, want :8080", cfg.Address)
	}
	if cfg.SecureCookie {
		t.Fatal("local HTTP origin must not require secure cookies")
	}
}

func TestLoadAPIUsesSecureCookiesForHTTPSOrigin(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("WEB_ORIGIN", "https://finance.investdx.biz.id")
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if !cfg.SecureCookie {
		t.Fatal("HTTPS origin must require secure cookies")
	}
}
