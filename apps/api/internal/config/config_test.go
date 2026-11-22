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

func TestLoadAPIRequiresGoogleCredentialsTogether(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	for name, env := range map[string][2]string{
		"id only":     {"client-id", ""},
		"secret only": {"", "client-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLIENT_ID", env[0])
			t.Setenv("GOOGLE_CLIENT_SECRET", env[1])
			if _, err := LoadAPI(); err == nil {
				t.Fatal("LoadAPI accepted partial Google credentials")
			}
		})
	}
}

func TestLoadAPIAcceptsGoogleCredentials(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "client-secret")
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if cfg.GoogleClientID != "client-id" || cfg.GoogleClientSecret != "client-secret" {
		t.Fatalf("Google credentials = %q/%q", cfg.GoogleClientID, cfg.GoogleClientSecret)
	}
}

func TestLoadAPIAllowsGoogleLoginDisabled(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	if _, err := LoadAPI(); err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
}

func TestLoadAPIWebOriginDefaultsToServedPort(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("WEB_ORIGIN", "")
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if cfg.WebOrigin != "http://localhost:8080" {
		t.Fatalf("WebOrigin = %q, want http://localhost:8080", cfg.WebOrigin)
	}
}

func TestLoadAPIUsesSecureCookiesForHTTPSOrigin(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("WEB_ORIGIN", "https://finance.musaddiqr.com")
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if !cfg.SecureCookie {
		t.Fatal("HTTPS origin must require secure cookies")
	}
}
