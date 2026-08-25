package httpmw

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDAndAccessLog(t *testing.T) {
	var output strings.Builder
	handler := RequestID(AccessLog(slog.New(slog.NewJSONHandler(&output, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(RequestIDHeader) != "valid-request-1" {
			t.Fatal("request ID was not propagated")
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(RequestIDHeader, "valid-request-1")
	handler.ServeHTTP(response, request)
	if response.Header().Get(RequestIDHeader) != "valid-request-1" || !strings.Contains(output.String(), `"status":204`) {
		t.Fatalf("missing correlation response or log: %s", output.String())
	}
}

func TestSameOriginRejectsCrossOriginMutation(t *testing.T) {
	handler := SameOrigin("https://finance.example", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestLimiter(t *testing.T) {
	limiter := NewLimiter(2, time.Minute)
	handler := limiter.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	for attempt := 1; attempt <= 3; attempt++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/login", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		handler.ServeHTTP(response, request)
		if attempt < 3 && response.Code != http.StatusOK {
			t.Fatalf("attempt %d unexpectedly rejected", attempt)
		}
		if attempt == 3 && response.Code != http.StatusTooManyRequests {
			t.Fatalf("third attempt got status %d", response.Code)
		}
	}
}
