package emailingress

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var recipientPattern = regexp.MustCompile(`^h_[a-f0-9]{32}@([a-z0-9.-]+)$`)

type SignedRequest struct {
	Timestamp    int64
	Recipient    string
	EnvelopeFrom string
	ContentHash  [32]byte
	Signature    []byte
	MessageID    string
	ObjectKey    string
}

func parseSignedHeaders(recipient, envelopeFrom, timestamp, hash, signature, messageID, objectKey, domain string) (SignedRequest, error) {
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	envelopeFrom = strings.TrimSpace(envelopeFrom)
	if recipient == "" || envelopeFrom == "" || timestamp == "" || hash == "" || signature == "" || objectKey == "" {
		return SignedRequest{}, fmt.Errorf("missing ingress headers")
	}
	if !recipientPattern.MatchString(recipient) {
		return SignedRequest{}, fmt.Errorf("invalid recipient")
	}
	if !strings.HasSuffix(recipient, "@"+strings.ToLower(strings.TrimSpace(domain))) {
		return SignedRequest{}, fmt.Errorf("recipient domain rejected")
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return SignedRequest{}, fmt.Errorf("invalid timestamp")
	}
	if len(hash) != sha256.Size*2 || hash != strings.ToLower(hash) {
		return SignedRequest{}, fmt.Errorf("invalid content hash")
	}
	decodedHash, err := hex.DecodeString(strings.ToLower(hash))
	if err != nil {
		return SignedRequest{}, fmt.Errorf("invalid content hash")
	}
	decodedSig, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(decodedSig) != sha256.Size || strings.TrimSpace(signature) != strings.ToLower(strings.TrimSpace(signature)) {
		return SignedRequest{}, fmt.Errorf("invalid signature")
	}
	var contentHash [32]byte
	copy(contentHash[:], decodedHash)
	return SignedRequest{Timestamp: ts, Recipient: recipient, EnvelopeFrom: envelopeFrom, ContentHash: contentHash, Signature: decodedSig, MessageID: strings.TrimSpace(messageID), ObjectKey: strings.TrimSpace(objectKey)}, nil
}

func (s SignedRequest) verify(body []byte, secret string, now time.Time) error {
	if secret == "" {
		return fmt.Errorf("ingress secret is not configured")
	}
	if delta := now.Unix() - s.Timestamp; delta < -300 || delta > 300 {
		return fmt.Errorf("stale timestamp")
	}
	digest := sha256.Sum256(body)
	if digest != s.ContentHash {
		return fmt.Errorf("content sha mismatch")
	}
	canonical := strconv.FormatInt(s.Timestamp, 10) + "\n" + s.Recipient + "\n" + s.EnvelopeFrom + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	if !hmac.Equal(mac.Sum(nil), s.Signature) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func parseAddress(value string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(address.Address) == "" {
		return "", fmt.Errorf("invalid sender")
	}
	return strings.ToLower(strings.TrimSpace(address.Address)), nil
}

func authTrusted(results string, trusted []string) bool {
	for _, result := range strings.Split(strings.ToLower(results), "\n") {
		result = strings.TrimSpace(result)
		for _, id := range trusted {
			id = strings.ToLower(strings.TrimSpace(id))
			if id == "" || (!strings.HasPrefix(result, id+";") && result != id) {
				continue
			}
			if strings.Contains(result, "dkim=pass") && strings.Contains(result, "dmarc=pass") {
				return true
			}
		}
	}
	return false
}
