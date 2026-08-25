package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeContentSerializesStructuredDataAsText(t *testing.T) {
	content, err := normalizeContent(map[string]any{"message": "makan 50rb"})
	if err != nil {
		t.Fatalf("normalizeContent() error = %v", err)
	}
	text, ok := content.(string)
	if !ok {
		t.Fatalf("content type = %T", content)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil || decoded["message"] != "makan 50rb" {
		t.Fatalf("decoded = %#v, error = %v", decoded, err)
	}
}

func TestGatewayErrorNeverIncludesSecretOrResponseBody(t *testing.T) {
	const apiKey = "test-secret-api-key"
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		http.Error(w, "provider leaked "+apiKey, http.StatusBadGateway)
	}))
	defer server.Close()
	client := New(server.URL, apiKey, "test-model")
	_, err := client.Structured(context.Background(), "request-id", "test", "system", "input", map[string]any{"type": "object"}, &map[string]any{})
	if err == nil || strings.Contains(err.Error(), apiKey) || strings.Contains(err.Error(), "provider leaked") {
		t.Fatalf("unsafe gateway error: %v", err)
	}
	if got := <-authorization; got != "Bearer "+apiKey {
		t.Fatalf("authorization = %q", got)
	}
}
