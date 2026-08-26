package gmail

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"google.golang.org/api/idtoken"
)

type tokenClaims struct {
	Email         string
	EmailVerified bool
}

func (h *Handler) ConfigurePubSub(audience, serviceAccount string) {
	h.pubsubAudience = audience
	h.pubsubServiceAccount = strings.ToLower(strings.TrimSpace(serviceAccount))
	h.verifyToken = func(ctx context.Context, raw, audience string) (tokenClaims, error) {
		payload, err := idtoken.Validate(ctx, raw, audience)
		if err != nil {
			return tokenClaims{}, err
		}
		email, _ := payload.Claims["email"].(string)
		verified, _ := payload.Claims["email_verified"].(bool)
		return tokenClaims{Email: strings.ToLower(email), EmailVerified: verified}, nil
	}
}

func (h *Handler) PubSub(w http.ResponseWriter, r *http.Request) {
	if h.pubsubAudience == "" || h.pubsubServiceAccount == "" || h.verifyToken == nil {
		http.Error(w, "Gmail Pub/Sub unavailable", http.StatusServiceUnavailable)
		return
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	claims, err := h.verifyToken(r.Context(), strings.TrimPrefix(authorization, "Bearer "), h.pubsubAudience)
	if err != nil || !claims.EmailVerified || claims.Email != h.pubsubServiceAccount {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid notification", 400)
		return
	}
	messageID, notification, err := decodePushNotification(raw, r.Header)
	if err != nil || !strings.EqualFold(notification.EmailAddress, h.mailbox) {
		slog.Warn("Gmail Pub/Sub notification rejected", "decode_error", errorLabel(err), "mailbox_match", err == nil && strings.EqualFold(notification.EmailAddress, h.mailbox), "body_bytes", len(raw), "json_body", json.Valid(raw), "message_id_header", strings.TrimSpace(r.Header.Get("X-Goog-Pubsub-Message-Id")) != "", "content_type", cleanHeader(r.Header.Get("Content-Type"), 80), "shape", jsonShape(raw, 0))
		http.Error(w, "invalid notification", 400)
		return
	}
	if err := h.captureNotification(r.Context(), messageID, notification.HistoryID, raw); err != nil {
		http.Error(w, "notification processing failed", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type gmailPushNotification struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    string `json:"historyId"`
}

func (n *gmailPushNotification) UnmarshalJSON(raw []byte) error {
	var fields struct {
		EmailAddress string          `json:"emailAddress"`
		HistoryID    json.RawMessage `json:"historyId"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var historyID string
	if json.Unmarshal(fields.HistoryID, &historyID) != nil {
		var number json.Number
		if json.Unmarshal(fields.HistoryID, &number) != nil {
			return fmt.Errorf("history ID must be a string or integer")
		}
		historyID = number.String()
	}
	if !validHistoryID(historyID) {
		return fmt.Errorf("history ID must contain only digits")
	}
	n.EmailAddress = fields.EmailAddress
	n.HistoryID = historyID
	return nil
}

func validHistoryID(value string) bool {
	if len(value) == 0 || len(value) > 30 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func decodePushNotification(raw []byte, header http.Header) (string, gmailPushNotification, error) {
	var envelope struct {
		Message struct {
			Data            string `json:"data"`
			MessageID       string `json:"messageId"`
			LegacyMessageID string `json:"message_id"`
		} `json:"message"`
	}
	decoded := raw
	messageID := strings.TrimSpace(header.Get("X-Goog-Pubsub-Message-Id"))
	if json.Unmarshal(raw, &envelope) == nil && envelope.Message.Data != "" {
		var err error
		decoded, err = decodeBase64Value(envelope.Message.Data)
		if err != nil {
			return "", gmailPushNotification{}, fmt.Errorf("decode wrapped data")
		}
		messageID = strings.TrimSpace(envelope.Message.MessageID)
		if messageID == "" {
			messageID = strings.TrimSpace(envelope.Message.LegacyMessageID)
		}
	}
	notification, err := decodeGmailNotification(decoded, 0)
	if err != nil {
		return "", gmailPushNotification{}, err
	}
	if messageID == "" {
		digest := sha256.Sum256(decoded)
		messageID = "payload-" + hex.EncodeToString(digest[:])
	}
	return messageID, notification, nil
}

func decodeGmailNotification(raw []byte, depth int) (gmailPushNotification, error) {
	if depth > 3 {
		return gmailPushNotification{}, fmt.Errorf("notification_nesting")
	}
	var notification gmailPushNotification
	if json.Unmarshal(raw, &notification) == nil && strings.TrimSpace(notification.EmailAddress) != "" && strings.TrimSpace(notification.HistoryID) != "" {
		return notification, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for _, key := range []string{"message", "data"} {
			child, ok := object[key]
			if !ok {
				continue
			}
			var encoded string
			if json.Unmarshal(child, &encoded) == nil {
				decoded, err := decodeBase64Value(encoded)
				if err == nil {
					if result, childErr := decodeGmailNotification(decoded, depth+1); childErr == nil {
						return result, nil
					}
				}
			} else if result, childErr := decodeGmailNotification(child, depth+1); childErr == nil {
				return result, nil
			}
		}
	}
	if decoded, err := decodeBase64Value(strings.TrimSpace(string(raw))); err == nil {
		if result, childErr := decodeGmailNotification(decoded, depth+1); childErr == nil {
			return result, nil
		}
	}
	return gmailPushNotification{}, fmt.Errorf("notification_json")
}

func decodeBase64Value(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	encodings := []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("base64_data")
}

func errorLabel(err error) string {
	if err == nil {
		return "none"
	}
	return cleanHeader(err.Error(), 80)
}
func cleanHeader(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func jsonShape(raw []byte, depth int) string {
	if depth > 2 {
		return "depth-limit"
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return "non-object"
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := "keys=" + strings.Join(keys, ",")
	for _, key := range []string{"message", "data"} {
		child, ok := object[key]
		if !ok {
			continue
		}
		var encoded string
		if json.Unmarshal(child, &encoded) == nil {
			decoded, err := decodeBase64Value(encoded)
			if err != nil {
				result += ";" + key + "=string:" + fmt.Sprint(len(encoded))
				continue
			}
			result += ";" + key + "=b64:" + fmt.Sprint(len(decoded)) + ":" + jsonShape(decoded, depth+1)
		} else {
			result += ";" + key + "={" + jsonShape(child, depth+1) + "}"
		}
	}
	return cleanHeader(result, 300)
}

func (h *Handler) captureNotification(ctx context.Context, messageID, historyID string, raw []byte) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id FROM gmail_integration WHERE mailbox=$1 AND status IN ('CONNECTED','WATCH_ACTIVE')`, h.mailbox).Scan(&householdID); err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	var sourceID string
	err = tx.QueryRow(ctx, `INSERT INTO source_event (household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES ($1,'SYSTEM',$2,now(),$3,'RECEIVED') ON CONFLICT DO NOTHING RETURNING id`, householdID, "gmail-pubsub:"+messageID, digest[:]).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("create Gmail source event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO source_event_payload (source_event_id,payload_json) VALUES ($1,$2::jsonb)`, sourceID, string(raw)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job (type,payload_json) VALUES ('PROCESS_GMAIL_HISTORY',jsonb_build_object('source_event_id',$1::uuid,'history_id',$2::text))`, sourceID, historyID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
