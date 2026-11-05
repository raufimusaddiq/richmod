package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGatewayFailureClasses(t *testing.T) {
	// A refused connection is unavailability, not a timeout and not a rejection.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	_, err := New(url, "key", "model").AgentTurn(context.Background(), "r", AgentRequest{SystemPrompt: "s", Content: "x"})
	if !IsUnavailable(err) || IsRejected(err) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refused connection must classify as unavailable, got %v", err)
	}
	for code, want := range map[int][2]bool{503: {true, false}, 502: {true, false}, 429: {true, false}, 400: {false, true}, 401: {false, true}, 422: {false, true}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		_, err := New(server.URL, "key", "model").AgentTurn(context.Background(), "r", AgentRequest{SystemPrompt: "s", Content: "x"})
		server.Close()
		if IsUnavailable(err) != want[0] || IsRejected(err) != want[1] {
			t.Fatalf("HTTP %d: unavailable=%v rejected=%v, want %v", code, IsUnavailable(err), IsRejected(err), want)
		}
	}
	if _, err := New("", "", "").AgentTurn(context.Background(), "r", AgentRequest{}); !IsUnavailable(err) {
		t.Fatalf("an unconfigured gateway is unavailable, got %v", err)
	}
	// The caller's own deadline is a timeout, never unavailability.
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = New(slow.URL, "key", "model").AgentTurn(ctx, "r", AgentRequest{SystemPrompt: "s", Content: "x"})
	if IsUnavailable(err) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline must stay a timeout, got %v", err)
	}
}
