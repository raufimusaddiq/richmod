package push

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// The expected record was produced by an independent Python cryptography
// implementation of RFC 8291/RFC 8188 for the fixed keys below.
func TestEncryptRecordMatchesIndependentVector(t *testing.T) {
	uaPublic := mustDecode(t, "BFFcPW6545a5BNP-yn9U_c0MwemXvzddylFa0KbDtANfRTa-OlDzGPv5pUdZAqIhUCvvDVfgjFOyzApW8X2fk1Q")
	auth := mustDecode(t, "AAECAwQFBgcICQoLDA0ODw")
	ephemeral, err := ecdh.P256().NewPrivateKey(mustDecode(t, "ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0-P0A"))
	if err != nil {
		t.Fatal(err)
	}
	salt := mustDecode(t, "ZGVmZ2hpamtsbW5vcHFycw")
	body, err := encryptRecord([]byte("hello richmod"), uaPublic, auth, ephemeral, salt)
	if err != nil {
		t.Fatal(err)
	}
	expected := "6465666768696a6b6c6d6e6f707172730000100041041f140146bfb1b251f84f4ddbe0d4cdcfd77afd984a9520e35794021f8312bb9eec995a08b1fa7704df3dcc0b50a9665263fb7711f95f9f8a449c5096e47c892b08c7c0aa4af0d883e17f192b0124c8a4d6d4f0154a7cc2d8a6c71720a87e"
	if hex.EncodeToString(body) != expected {
		t.Fatalf("unexpected record:\n got %s\nwant %s", hex.EncodeToString(body), expected)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestSendSuccessAndDeadSubscription(t *testing.T) {
	uaPrivate, err := ecdh.P256().NewPrivateKey(mustDecode(t, "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"))
	if err != nil {
		t.Fatal(err)
	}
	vapid, err := NewVAPID("ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0-P0A", "mailto:test@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	var status atomic.Int32
	status.Store(http.StatusCreated)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(request.Header.Get("Authorization"), "vapid t=") {
			t.Errorf("missing VAPID authorization")
		}
		if request.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("content encoding=%q", request.Header.Get("Content-Encoding"))
		}
		return &http.Response{StatusCode: int(status.Load()), Body: http.NoBody, Header: make(http.Header), Request: request}, nil
	})}
	subscription := Subscription{
		Endpoint: "https://1.1.1.1/device",
		P256DH:   base64.RawURLEncoding.EncodeToString(uaPrivate.PublicKey().Bytes()),
		Auth:     "AAECAwQFBgcICQoLDA0ODw",
	}
	if err := vapid.Send(context.Background(), client, subscription, []byte(`{"title":"x"}`)); err != nil {
		t.Fatalf("send: %v", err)
	}
	status.Store(http.StatusGone)
	err = vapid.Send(context.Background(), client, subscription, []byte(`{"title":"x"}`))
	var delivery *DeliveryError
	if !asDeliveryError(err, &delivery) || !delivery.Dead {
		t.Fatalf("expected dead delivery error, got %v", err)
	}
}

func TestPublicKeyDerivation(t *testing.T) {
	private := mustDecode(t, "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA")
	if got := PublicKey(base64.RawURLEncoding.EncodeToString(private)); got != "BFFcPW6545a5BNP-yn9U_c0MwemXvzddylFa0KbDtANfRTa-OlDzGPv5pUdZAqIhUCvvDVfgjFOyzApW8X2fk1Q" {
		t.Fatalf("public key=%q", got)
	}
	if got := PublicKey(""); got != "" {
		t.Fatalf("empty key=%q", got)
	}
}

func asDeliveryError(err error, target **DeliveryError) bool {
	delivery, ok := err.(*DeliveryError)
	if ok {
		*target = delivery
	}
	return ok
}

func mustDecode(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
