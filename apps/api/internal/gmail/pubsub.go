package gmail

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	var envelope struct {
		Message struct {
			Data      string `json:"data"`
			MessageID string `json:"messageId"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Message.MessageID == "" {
		http.Error(w, "invalid notification", 400)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		http.Error(w, "invalid notification", 400)
		return
	}
	var notification struct {
		EmailAddress string `json:"emailAddress"`
		HistoryID    string `json:"historyId"`
	}
	if err := json.Unmarshal(decoded, &notification); err != nil || notification.HistoryID == "" || !strings.EqualFold(notification.EmailAddress, h.mailbox) {
		http.Error(w, "invalid notification", 400)
		return
	}
	if err := h.captureNotification(r.Context(), envelope.Message.MessageID, notification.HistoryID, raw); err != nil {
		http.Error(w, "notification processing failed", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	err = tx.QueryRow(ctx, `INSERT INTO source_event (household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES ($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') ON CONFLICT DO NOTHING RETURNING id`, householdID, "gmail-pubsub:"+messageID, digest[:]).Scan(&sourceID)
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
