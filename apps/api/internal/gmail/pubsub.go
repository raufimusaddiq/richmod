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
		slog.Warn("Gmail Pub/Sub notification rejected", "decode_error", errorLabel(err), "mailbox_match", err == nil && strings.EqualFold(notification.EmailAddress, h.mailbox), "body_bytes", len(raw), "json_body", json.Valid(raw), "message_id_header", strings.TrimSpace(r.Header.Get("X-Goog-Pubsub-Message-Id")) != "", "content_type", cleanHeader(r.Header.Get("Content-Type"), 80))
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
		decoded, err = base64.StdEncoding.DecodeString(envelope.Message.Data)
		if err != nil {
			return "", gmailPushNotification{}, fmt.Errorf("decode wrapped data")
		}
		messageID = strings.TrimSpace(envelope.Message.MessageID)
		if messageID == "" {
			messageID = strings.TrimSpace(envelope.Message.LegacyMessageID)
		}
	}
	notification, err := decodeGmailNotification(decoded)
	if err != nil {
		return "", gmailPushNotification{}, err
	}
	if messageID == "" {
		digest := sha256.Sum256(decoded)
		messageID = "payload-" + hex.EncodeToString(digest[:])
	}
	return messageID, notification, nil
}

func decodeGmailNotification(raw []byte) (gmailPushNotification, error) {
	var notification gmailPushNotification
	if json.Unmarshal(raw, &notification) == nil && strings.TrimSpace(notification.EmailAddress) != "" && strings.TrimSpace(notification.HistoryID) != "" {
		return notification, nil
	}
	encoded := strings.TrimSpace(string(raw))
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(encoded)
		if err == nil && json.Unmarshal(decoded, &notification) == nil && strings.TrimSpace(notification.EmailAddress) != "" && strings.TrimSpace(notification.HistoryID) != "" {
			return notification, nil
		}
	}
	return gmailPushNotification{}, fmt.Errorf("notification_json")
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
