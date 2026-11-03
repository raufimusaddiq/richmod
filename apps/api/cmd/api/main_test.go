package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/raufimusaddiq/richmod/apps/api/internal/webui"
)

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	securityHeaders(next).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := response.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("Content-Security-Policy must be set")
	}
}

func TestAPIOwnedPath(t *testing.T) {
	for _, path := range []string{"/api", "/api/v1/transactions", "/webhooks/telegram", "/finance/v1/email/inbond", "/healthz", "/readyz"} {
		if !apiOwnedPath(path) {
			t.Errorf("apiOwnedPath(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/", "/transactions", "/apiary", "/webhooks2", "/health", "/healthz/extra"} {
		if apiOwnedPath(path) {
			t.Errorf("apiOwnedPath(%q) = true, want false", path)
		}
	}
}

func TestUISecurityHeaders(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":                 {Data: []byte("home")},
		"_next/static/chunks/app.js": {Data: []byte("js")},
	}
	handler := newUIHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), webui.NewHandler(assets))
	for _, path := range []string{"/", "/_next/static/chunks/app.js"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
			for header, want := range map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"Referrer-Policy":        "same-origin",
			} {
				if got := response.Header().Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			if csp := response.Header().Get("Content-Security-Policy"); csp != "frame-ancestors 'none'" {
				t.Fatalf("Content-Security-Policy = %q, want frame-ancestors 'none'", csp)
			}
			if strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src") {
				t.Fatal("UI CSP must not block the export's own scripts")
			}
		})
	}
}
