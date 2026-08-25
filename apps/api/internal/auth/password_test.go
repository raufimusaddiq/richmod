package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("a-long-bootstrap-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	valid, err := VerifyPassword(hash, "a-long-bootstrap-password")
	if err != nil || !valid {
		t.Fatalf("VerifyPassword() = %t, %v; want true, nil", valid, err)
	}
	valid, err = VerifyPassword(hash, "another-password")
	if err != nil || valid {
		t.Fatalf("VerifyPassword() = %t, %v; want false, nil", valid, err)
	}
}

func TestHashPasswordRejectsShortPassword(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("HashPassword accepted a short password")
	}
}
