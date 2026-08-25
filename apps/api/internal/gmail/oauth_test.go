package gmail

import (
	"bytes"
	"testing"
)

func TestRefreshTokenEncryptionIsHouseholdBound(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	ciphertext, err := encrypt(key, "household-a", "refresh-secret")
	if err != nil {
		t.Fatalf("encrypt() error = %v", err)
	}
	plaintext, err := decrypt(key, "household-a", ciphertext)
	if err != nil || plaintext != "refresh-secret" {
		t.Fatalf("decrypt() = %q, %v", plaintext, err)
	}
	if _, err := decrypt(key, "household-b", ciphertext); err == nil {
		t.Fatal("token decrypted for a different household")
	}
}

func TestNewHandlerRejectsShortEncryptionKey(t *testing.T) {
	if _, err := NewHandler(nil, OAuthClient{}, "mailbox@example.com", "abcd"); err == nil {
		t.Fatal("short encryption key was accepted")
	}
}
