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

// authTrusted decides whether a forwarded message's sender authentication can
// be relied on. Only the topmost Authentication-Results header (and the topmost
// ARC-Authentication-Results header) counts: the receiving MTA prepends its
// own, so every header below it may be sender-written (RFC 8601 section 5).
// That header must come from a trusted authserv-id and report dkim=pass and
// dmarc=pass as method results, and a DMARC header.from, when present, must be
// the From domain Richmod routes on.
func authTrusted(email ParsedEmail, trusted []string) bool {
	return authHeaderTrusted(topmost(email.AuthenticationResultsHeaders), false, email.Sender, trusted) ||
		authHeaderTrusted(topmost(email.ARCAuthenticationResultsHeaders), true, email.Sender, trusted)
}

func topmost(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func authHeaderTrusted(value string, arc bool, sender string, trusted []string) bool {
	parts := strings.Split(stripHeaderComments(strings.ToLower(value)), ";")
	if arc && len(parts) > 0 && strings.HasPrefix(strings.TrimSpace(parts[0]), "i=") {
		parts = parts[1:]
	}
	if len(parts) < 2 {
		return false
	}
	// authserv-id may be followed by a version number.
	fields := strings.Fields(parts[0])
	if len(fields) == 0 || !trustedAuthserv(fields[0], trusted) {
		return false
	}
	senderDomain := ""
	if at := strings.LastIndex(sender, "@"); at >= 0 {
		senderDomain = strings.ToLower(strings.TrimSpace(sender[at+1:]))
	}
	dkimPass, dmarcPass := false, false
	for _, resinfo := range parts[1:] {
		tokens := strings.Fields(resinfo)
		if len(tokens) == 0 {
			continue
		}
		method, result, ok := strings.Cut(tokens[0], "=")
		if !ok || result != "pass" {
			continue
		}
		switch method {
		case "dkim":
			dkimPass = true
		case "dmarc":
			aligned := true
			for _, property := range tokens[1:] {
				if from, found := strings.CutPrefix(property, "header.from="); found {
					aligned = senderDomain != "" && strings.Trim(from, `"`) == senderDomain
				}
			}
			dmarcPass = dmarcPass || aligned
		}
	}
	return dkimPass && dmarcPass
}

func trustedAuthserv(id string, trusted []string) bool {
	for _, candidate := range trusted {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate != "" && candidate == id {
			return true
		}
	}
	return false
}

// stripHeaderComments removes RFC 5322 comments, which may contain any text,
// including look-alike "dkim=pass" fragments.
func stripHeaderComments(value string) string {
	var out strings.Builder
	depth := 0
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			escaped = false
			if depth == 0 {
				out.WriteRune(r)
			}
		case r == '\\':
			escaped = true
			if depth == 0 {
				out.WriteRune(r)
			}
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0:
			out.WriteRune(r)
		}
	}
	return out.String()
}
