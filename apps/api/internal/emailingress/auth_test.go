package emailingress

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
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
	trusted := []string{"mx.example"}
	email := func(sender string, authResults ...string) ParsedEmail {
		return ParsedEmail{Sender: sender, AuthenticationResultsHeaders: authResults}
	}
	cases := []struct {
		name  string
		email ParsedEmail
		want  bool
	}{
		{"trusted pass", email("alerts@bank.test", "mx.example; dkim=pass; dmarc=pass"), true},
		{"aligned header.from", email("alerts@bank.test", "mx.example; dkim=pass header.d=bank.test; dmarc=pass header.from=bank.test policy.dmarc=reject"), true},
		{"authserv version", email("alerts@bank.test", "mx.example 1; dkim=pass; dmarc=pass"), true},
		{"failed DKIM", email("alerts@bank.test", "mx.example; dkim=fail; dmarc=pass"), false},
		{"untrusted authserv", email("alerts@bank.test", "attacker.test; dkim=pass; dmarc=pass"), false},
		{"authserv prefix only", email("alerts@bank.test", "mx.example.attacker.test; dkim=pass; dmarc=pass"), false},
		{"misaligned header.from", email("alerts@bank.test", "mx.example; dkim=pass; dmarc=pass header.from=attacker.test"), false},
		{"pass only inside a comment", email("alerts@bank.test", "mx.example; dkim=none (dkim=pass); dmarc=none (dmarc=pass)"), false},
		{"pass only as a substring", email("alerts@bank.test", "mx.example; xdkim=pass; spf=pass reason=dmarc=pass"), false},
		// The receiving MTA prepends its header; a sender-written trusted one
		// below it must not be believed.
		{"forged header below the receiver's", email("alerts@bank.test", "mx.example; dkim=none; dmarc=fail", "mx.example; dkim=pass; dmarc=pass"), false},
		{"forged header below an untrusted receiver", email("alerts@bank.test", "relay.test; dkim=none", "mx.example; dkim=pass; dmarc=pass"), false},
		{"trusted ARC instance", ParsedEmail{Sender: "alerts@bank.test", ARCAuthenticationResultsHeaders: []string{"i=2; mx.example; dkim=pass; dmarc=pass header.from=bank.test", "i=1; mx.example; dkim=fail"}}, true},
		{"no headers", ParsedEmail{Sender: "alerts@bank.test"}, false},
	}
	for _, tc := range cases {
		if got := authTrusted(tc.email, trusted); got != tc.want {
			t.Errorf("%s: authTrusted = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseMIMEKeepsAuthenticationHeaderOrder(t *testing.T) {
	raw := []byte("Authentication-Results: mx.example; dkim=fail; dmarc=fail\r\nAuthentication-Results: mx.example; dkim=pass; dmarc=pass\r\nFrom: Bank <alerts@bank.test>\r\nContent-Type: text/plain\r\n\r\nAmount 100")
	parsed, err := parseMIME(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.AuthenticationResultsHeaders) != 2 || !strings.Contains(parsed.AuthenticationResultsHeaders[0], "dkim=fail") {
		t.Fatalf("headers = %q", parsed.AuthenticationResultsHeaders)
	}
	if authTrusted(parsed, []string{"mx.example"}) {
		t.Fatal("a sender-written trusted header below the receiver's was believed")
	}
}
