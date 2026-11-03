// Package pushsupport holds the VAPID configuration predicate and the
// subscription upsert shared by the API and worker binaries. Keeping the
// enabled decision in one place prevents the API from advertising a state the
// sender will never run in.
package pushsupport

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"time"
)

// ErrDisabled is returned when the VAPID configuration is absent or invalid.
var ErrDisabled = errors.New("push disabled: VAPID_PRIVATE_KEY or VAPID_SUBJECT is not configured")

// VAPID holds the parsed signing key and public key.
type VAPID struct {
	Private   *ecdh.PrivateKey
	Signing   *ecdsa.PrivateKey
	Public    []byte
	PublicKey string
	Subject   string
}

// Parse validates the private scalar and subject exactly once. Both the API's
// enabled state and the worker's sender construction go through this function.
func Parse(privateKey, subject string) (*VAPID, error) {
	privateKey = strings.TrimSpace(privateKey)
	subject = strings.TrimSpace(subject)
	if privateKey == "" || subject == "" || !validSubject(subject) {
		return nil, ErrDisabled
	}
	raw, err := decodeBase64(privateKey)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("VAPID_PRIVATE_KEY must be a base64url raw P-256 private key")
	}
	private, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return nil, errors.New("VAPID_PRIVATE_KEY is not a valid P-256 private key")
	}
	x, y := elliptic.P256().ScalarBaseMult(raw)
	signing := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D:         new(big.Int).SetBytes(raw),
	}
	public := private.PublicKey().Bytes()
	return &VAPID{
		Private:   private,
		Signing:   signing,
		Public:    public,
		PublicKey: base64.RawURLEncoding.EncodeToString(public),
		Subject:   subject,
	}, nil
}

// Enabled is the single predicate used by the API and worker.
func Enabled(privateKey, subject string) bool {
	_, err := Parse(privateKey, subject)
	return err == nil
}

// PublicKey derives the advertised key. It returns empty when the private key
// is invalid; callers must gate on Enabled/Parse before trusting it.
func PublicKey(privateKey string) string {
	config, err := Parse(privateKey, "https://richmod.invalid")
	if err != nil {
		return ""
	}
	return config.PublicKey
}

// Authorization builds the VAPID Authorization header for one endpoint.
func (v *VAPID) Authorization(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid endpoint")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		"aud": parsed.Scheme + "://" + parsed.Host,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": v.Subject,
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, v.Signing, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return "vapid t=" + signingInput + "." + base64.RawURLEncoding.EncodeToString(signature) + ", k=" + v.PublicKey, nil
}

func validSubject(subject string) bool {
	if strings.HasPrefix(subject, "mailto:") {
		return strings.TrimSpace(strings.TrimPrefix(subject, "mailto:")) != ""
	}
	if strings.HasPrefix(subject, "https://") || strings.HasPrefix(subject, "http://") {
		parsed, err := url.Parse(subject)
		return err == nil && parsed.Host != "" && parsed.User == nil
	}
	return false
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}
