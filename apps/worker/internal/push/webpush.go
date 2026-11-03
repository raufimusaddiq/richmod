// Package push implements Web Push (RFC 8030) delivery with VAPID (RFC 8292)
// and aes128gcm payload encryption (RFC 8188/RFC 8291) using only the standard
// library.
package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"
)

const recordSize = 4096

// VAPID wraps the shared VAPID configuration so the sender and the API use the
// same enabled predicate.
type VAPID struct{ config *pushsupport.VAPID }

// NewVAPID parses a base64url raw P-256 private scalar and the VAPID subject.
func NewVAPID(privateKey, subject string) (*VAPID, error) {
	config, err := pushsupport.Parse(privateKey, subject)
	if err != nil {
		return nil, err
	}
	return &VAPID{config: config}, nil
}

// PublicKey returns the base64url uncompressed P-256 public key.
func (v *VAPID) PublicKey() string { return v.config.PublicKey }

// PublicKey derives the advertised key from a private key environment value.
func PublicKey(privateKey string) string { return pushsupport.PublicKey(privateKey) }

// Subscription is one browser push subscription.
type Subscription struct {
	ID       string
	Endpoint string
	P256DH   string
	Auth     string
}

// DeliveryError describes a failed push attempt without carrying the endpoint.
type DeliveryError struct {
	Status  int
	Dead    bool
	Message string
}

func (e *DeliveryError) Error() string { return e.Message }

// Send encrypts payload for the subscription and posts it to the push service.
// A 2xx response means the push service accepted it for delivery.
func (v *VAPID) Send(ctx context.Context, client *http.Client, sub Subscription, payload []byte) error {
	if client == nil {
		client = secureClient()
	}
	if err := checkEndpoint(ctx, sub.Endpoint); err != nil {
		return &DeliveryError{Status: http.StatusBadRequest, Message: "subscription endpoint is not allowed"}
	}
	uaPublic, err := decodeBase64(sub.P256DH)
	if err != nil || len(uaPublic) != 65 || uaPublic[0] != 4 {
		return &DeliveryError{Status: http.StatusBadRequest, Message: "subscription has an invalid p256dh key"}
	}
	authSecret, err := decodeBase64(sub.Auth)
	if err != nil || len(authSecret) != 16 {
		return &DeliveryError{Status: http.StatusBadRequest, Message: "subscription has an invalid auth secret"}
	}
	body, err := encryptPayload(payload, uaPublic, authSecret)
	if err != nil {
		return &DeliveryError{Status: http.StatusInternalServerError, Message: "push payload encryption failed"}
	}
	authorization, err := v.config.Authorization(sub.Endpoint)
	if err != nil {
		return &DeliveryError{Status: http.StatusInternalServerError, Message: "VAPID authorization failed"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return &DeliveryError{Status: http.StatusBadRequest, Message: "subscription endpoint is invalid"}
	}
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("TTL", "86400")
	request.Header.Set("Urgency", "normal")
	request.Header.Set("Authorization", authorization)
	response, err := client.Do(request)
	if err != nil {
		return &DeliveryError{Status: 0, Message: "push service request failed"}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	dead := response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone
	return &DeliveryError{
		Status:  response.StatusCode,
		Dead:    dead,
		Message: fmt.Sprintf("push service returned %d", response.StatusCode),
	}
}

// encryptPayload builds one aes128gcm record. Ephemeral key and salt are
// generated per message; the deterministic variant below exists for tests.
func encryptPayload(payload, uaPublic, authSecret []byte) ([]byte, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encryptRecord(payload, uaPublic, authSecret, ephemeral, salt)
}

func encryptRecord(payload, uaPublic, authSecret []byte, ephemeral *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaKey, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	shared, err := ephemeral.ECDH(uaKey)
	if err != nil {
		return nil, err
	}
	asPublic := ephemeral.PublicKey().Bytes()

	prkKey := hmacSHA256(authSecret, shared)
	keyInfo := make([]byte, 0, len("WebPush: info\x00")+len(uaPublic)+len(asPublic))
	keyInfo = append(keyInfo, "WebPush: info\x00"...)
	keyInfo = append(keyInfo, uaPublic...)
	keyInfo = append(keyInfo, asPublic...)
	ikm := hmacSHA256(prkKey, append(keyInfo, 1))

	prk := hmacSHA256(salt, ikm)
	cek := hmacSHA256(prk, append([]byte("Content-Encoding: aes128gcm\x00"), 1))[:16]
	nonce := hmacSHA256(prk, append([]byte("Content-Encoding: nonce\x00"), 1))[:12]

	record := append(append([]byte(nil), payload...), 2) // last-record delimiter
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, record, nil)

	body := make([]byte, 0, 16+4+1+len(asPublic)+len(ciphertext))
	body = append(body, salt...)
	body = binary.BigEndian.AppendUint32(body, recordSize)
	body = append(body, byte(len(asPublic)))
	body = append(body, asPublic...)
	body = append(body, ciphertext...)
	return body, nil
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}
