package emailingress

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func TestSignedRequestVerify(t *testing.T) {
	body := []byte("From: Bank <alerts@example.test>\r\n\r\namount 100")
	digest := sha256.Sum256(body)
	ts := time.Now().UTC().Unix()
	recipient := "h_0123456789abcdef0123456789abcdef@richmod.link"
	envelope := "forwarder@example.test"
	canonical := strconv.FormatInt(ts, 10) + "\n" + recipient + "\n" + envelope + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(canonical))
	signed, err := parseSignedHeaders(recipient, envelope, strconv.FormatInt(ts, 10), hex.EncodeToString(digest[:]), hex.EncodeToString(mac.Sum(nil)), "<id>", "raw/key.eml", "richmod.link")
	if err != nil {
		t.Fatal(err)
	}
	if err := signed.verify(body, "secret", time.Unix(ts, 0)); err != nil {
		t.Fatal(err)
	}
	if err := signed.verify([]byte("tampered"), "secret", time.Unix(ts, 0)); err == nil {
		t.Fatal("tampered body accepted")
	}
	if err := signed.verify(body, "wrong", time.Unix(ts, 0)); err == nil {
		t.Fatal("wrong secret accepted")
	}
	tamperedRecipient := signed
	tamperedRecipient.Recipient = "h_ffffffffffffffffffffffffffffffff@richmod.link"
	if err := tamperedRecipient.verify(body, "secret", time.Unix(ts, 0)); err == nil {
		t.Fatal("changed recipient accepted with original signature")
	}
	tamperedEnvelope := signed
	tamperedEnvelope.EnvelopeFrom = "other@example.test"
	if err := tamperedEnvelope.verify(body, "secret", time.Unix(ts, 0)); err == nil {
		t.Fatal("changed envelope sender accepted with original signature")
	}
	tamperedHash := signed
	tamperedHash.ContentHash[0] ^= 0xff
	if err := tamperedHash.verify(body, "secret", time.Unix(ts, 0)); err == nil {
		t.Fatal("changed SHA accepted")
	}
	if err := signed.verify(body, "secret", time.Unix(ts+301, 0)); err == nil {
		t.Fatal("stale timestamp accepted")
	}
	if err := signed.verify(body, "secret", time.Unix(ts-301, 0)); err == nil {
		t.Fatal("future timestamp accepted")
	}
	if _, err := parseSignedHeaders(recipient, envelope, strconv.FormatInt(ts, 10), "not-hex", hex.EncodeToString(mac.Sum(nil)), "<id>", "raw/key.eml", "richmod.link"); err == nil {
		t.Fatal("invalid hash accepted")
	}
}

func TestRandomLocalPart(t *testing.T) {
	first, err := randomLocalPart()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomLocalPart()
	if err != nil {
		t.Fatal(err)
	}
	if !recipientPattern.MatchString(first + "@richmod.link") {
		t.Fatalf("invalid local part %q", first)
	}
	if first == second {
		t.Fatal("generated addresses are not unique")
	}
}

func TestAuthTrusted(t *testing.T) {
	if !authTrusted("mx.example; dkim=pass; dmarc=pass", []string{"mx.example"}) {
		t.Fatal("trusted result rejected")
	}
	if authTrusted("mx.example; dkim=fail; dmarc=pass", []string{"mx.example"}) {
		t.Fatal("failed DKIM accepted")
	}
}
