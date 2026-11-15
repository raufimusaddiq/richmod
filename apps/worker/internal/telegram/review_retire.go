package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RetireCardPayload is a RETIRE_TELEGRAM_REVIEW_CARD job, written by the migration 00079 trigger when the
// request closes and by BindReviewMessage for a card arriving after close.
type RetireCardPayload struct {
	ReviewRequestID string `json:"review_request_id"`
	RecipientID     string `json:"recipient_id"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int64  `json:"message_id"`
	Status          string `json:"status"`
	// Superseded marks a message replaced by a newer message of the same open review; it loses its buttons
	// without a closure note.
	Superseded bool `json:"superseded,omitempty"`
}

func DecodeRetireCardPayload(raw json.RawMessage) (RetireCardPayload, error) {
	var payload RetireCardPayload
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return RetireCardPayload{}, fmt.Errorf("decode retire card payload: %w", err)
	}
	if payload.ReviewRequestID == "" || payload.RecipientID == "" || payload.ChatID == 0 || payload.MessageID == 0 {
		return RetireCardPayload{}, fmt.Errorf("invalid retire card payload")
	}
	return payload, nil
}

// reviewClosureNote is the line a retired card gains; an active status has none (the request was renewed) and
// a resolved review names its outcome when known.
func reviewClosureNote(status, outcome string) string {
	switch status {
	case "RESOLVED":
		if outcome != "" {
			return "✅ Tinjauan ini sudah selesai: " + outcome + "."
		}
		return "✅ Tinjauan ini sudah selesai."
	case "CANCELLED":
		return "Tinjauan ini sudah ditutup."
	case "EXPIRED":
		return "⏳ Tombol tinjauan ini sudah kedaluwarsa. Tinjauannya masih ada di Kotak Tinjauan."
	}
	return ""
}

const supersededCardNote = "↪️ Dilanjutkan di pesan berikutnya."

// RetireReviewCard strips one delivered card's buttons after re-reading state; a renewed copy keeps its card.
// Gone or unreachable cards are finished; transient Telegram failures are returned for retry.
func (p *Processor) RetireReviewCard(ctx context.Context, bot *Bot, payload RetireCardPayload) error {
	var status, recipientStatus, recipientText string
	var boundMessageID int64
	err := p.pool.QueryRow(ctx, `SELECT r.status,rr.delivery_status,COALESCE(rr.telegram_message_id,0),COALESCE(rr.delivered_text,'')
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.id=$1 AND rr.id=$2 AND rr.telegram_chat_id=$3`, payload.ReviewRequestID, payload.RecipientID, payload.ChatID).Scan(&status, &recipientStatus, &boundMessageID, &recipientText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load retired Telegram review card: %w", err)
	}
	outcome := ""
	if status == "RESOLVED" {
		if outcome, err = reviewOutcome(ctx, p.pool, payload.ReviewRequestID); err != nil {
			return fmt.Errorf("load retired Telegram review outcome: %w", err)
		}
	}
	note := reviewClosureNote(status, outcome)
	switch {
	case note != "":
	case recipientStatus == "EXPIRED":
		// This copy's own window passed while the review is open elsewhere (or was reopened for a queued copy).
		note = reviewClosureNote("EXPIRED", "")
	case payload.Superseded && boundMessageID != payload.MessageID:
		// A newer message of the open review replaced this one.
		note = supersededCardNote
	default:
		// The card is live: the request was renewed or this is its current card.
		return nil
	}
	// A card settled in place (ADR-046) belongs to its outcome edits; a retirement claimed before the settle
	// superseded it must not strip the inline choice.
	var settled bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_conversation WHERE review_request_id=$1 AND COALESCE(context_json->'card_outcomes','{}'::jsonb) ? $2)`, payload.ReviewRequestID, cardOutcomeKey(payload.ChatID, payload.MessageID)).Scan(&settled); err != nil {
		return fmt.Errorf("load settled Telegram review card: %w", err)
	}
	if settled {
		return nil
	}
	// Each message keeps its own last text; the recipient row holds the current card's for cards bound before
	// review_request_message existed.
	var text string
	err = p.pool.QueryRow(ctx, `SELECT COALESCE(delivered_text,'') FROM review_request_message WHERE recipient_id=$1 AND telegram_chat_id=$2 AND telegram_message_id=$3`, payload.RecipientID, payload.ChatID, payload.MessageID).Scan(&text)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load retired Telegram review message: %w", err)
	}
	if text == "" && boundMessageID == payload.MessageID {
		text = recipientText
	}
	if text = retiredMessageText(text, note); text != "" {
		err = bot.Edit(ctx, EditPayload{ChatID: payload.ChatID, MessageID: payload.MessageID, Text: text})
		if err == nil || editFinished(err) {
			return nil
		}
		if !editRejected(err) {
			return err
		}
		// Telegram refused the text itself; removing the keyboard is enough.
	}
	err = bot.EditReplyMarkup(ctx, payload.ChatID, payload.MessageID)
	if err == nil || editFinished(err) {
		return nil
	}
	if editRejected(err) {
		return TerminalError(err)
	}
	return err
}

// ReviewCardLive reports whether a queued edit may still apply: once the request is terminal the card belongs
// to retirement and an old keyboard must not return.
func (p *Processor) ReviewCardLive(ctx context.Context, reviewRequestID string) (bool, error) {
	var live bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request WHERE id=$1 AND status IN ('PENDING_SEND','OPEN'))`, reviewRequestID).Scan(&live); err != nil {
		return false, fmt.Errorf("load edited Telegram review request: %w", err)
	}
	return live, nil
}

// Keeps the stored card text in step with a successful edit so a later retirement reproduces what Telegram shows.
func (p *Processor) RecordReviewCardText(ctx context.Context, chatID, messageID int64, text string) error {
	if _, err := p.pool.Exec(ctx, `UPDATE review_request_recipient SET delivered_text=NULLIF($3,'') WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, chatID, messageID, clean(text, 4000)); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `UPDATE review_request_message SET delivered_text=NULLIF($3,'') WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, chatID, messageID, clean(text, 4000))
	return err
}

// Records the separate merchant-learning question on the recipient row, creating it when the confirming chat
// never received the card.
func (p *Processor) BindMerchantLearningMessage(ctx context.Context, reviewRequestID string, chatID, messageID int64) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,merchant_learning_message_id,delivery_status) VALUES($1,$2,$3,'SKIPPED')
		ON CONFLICT (review_request_id,telegram_chat_id) DO UPDATE SET merchant_learning_message_id=EXCLUDED.merchant_learning_message_id`, reviewRequestID, chatID, messageID)
	if err != nil {
		return fmt.Errorf("bind merchant learning message: %w", err)
	}
	return nil
}
