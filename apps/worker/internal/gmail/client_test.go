package gmail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessTokenClassifiesOAuthRejectionWithoutLeakingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"secret refresh token"}`))
	}))
	defer server.Close()

	c := &client{oauth: oauthClient{ClientID: "id", ClientSecret: "secret"}, httpClient: server.Client(), tokenURL: server.URL}
	_, err := c.accessToken(context.Background(), "refresh-token")
	if err == nil || err.Error() != "Google token refresh rejected: invalid_grant (HTTP 400)" {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "secret refresh token") {
		t.Fatal("OAuth response detail leaked")
	}
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatal("expected permanent OAuth error")
	}
}

func TestAccessTokenKeepsServerFailureRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
	}))
	defer server.Close()

	c := &client{oauth: oauthClient{ClientID: "id", ClientSecret: "secret"}, httpClient: server.Client(), tokenURL: server.URL}
	_, err := c.accessToken(context.Background(), "refresh-token")
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || permanent.Permanent() {
		t.Fatalf("expected retryable OAuth error, got %v", err)
	}
}

func TestSafeOAuthErrorCodeRejectsUnstructuredContent(t *testing.T) {
	if got := safeOAuthErrorCode("invalid grant secret"); got != "oauth_error" {
		t.Fatalf("safeOAuthErrorCode = %q", got)
	}
	if got := safeOAuthErrorCode("invalid_grant"); got != "invalid_grant" {
		t.Fatalf("safeOAuthErrorCode = %q", got)
	}
}
