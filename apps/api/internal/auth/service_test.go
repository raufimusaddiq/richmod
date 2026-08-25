package auth

import "testing"

func TestNewSessionTokenIsOpaqueAndHasStableHash(t *testing.T) {
	token, hash, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}
	if len(token) < 40 {
		t.Fatalf("token is unexpectedly short: %d", len(token))
	}
	if string(hash) != string(hashToken(token)) {
		t.Fatal("stored token hash does not match token")
	}
}
