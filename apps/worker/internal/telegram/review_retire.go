package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RetireCardPayload is one RETIRE_TELEGRAM_REVIEW_CARD job: a delivered review
// card whose request left its active state. The review_request trigger from
// migration 00079 writes it in the transaction that closes the request, and
// BindReviewMessage writes it for a card that arrived after the close.
type RetireCardPayload struct {
	ReviewRequestID string `json:"review_request_id"`
	RecipientID     string `json:"recipient_id"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int64  `json:"message_id"`
	Status          string `json:"status"`
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

// reviewClosureNote is the line a retired card gains under its text. An active
// status has no note: the request was renewed and the card is live again.
func reviewClosureNote(status string) string {
	switch status {
	case "RESOLVED":
		return "✅ Tinjauan ini sudah selesai."
	case "CANCELLED":
		return "Tinjauan ini sudah ditutup."
	case "EXPIRED":
		return "Tombol tinjauan ini sudah kedaluwarsa. Buka Kotak Tinjauan untuk melanjutkan."
	}
	return ""
}

// RetireReviewCard removes the buttons from one delivered review card. The
// request is re-read first: an EXPIRED request that a reply renewed to OPEN keeps
// its card, and a later close queues a fresh retirement. When the card's text is
// known the closure note is appended; otherwise only the keyboard is removed.
// It never sends a new message. A card that is gone, already retired, or in a
// chat the bot cannot reach is finished; transient Telegram failures are
// returned so the queue retries.
func (p *Processor) RetireReviewCard(ctx context.Context, bot *Bot, payload RetireCardPayload) error {
	var status, text string
	var boundMessageID int64
	err := p.pool.QueryRow(ctx, `SELECT r.status,COALESCE(rr.telegram_message_id,0),COALESCE(rr.delivered_text,'')
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.id=$1 AND rr.id=$2 AND rr.telegram_chat_id=$3`, payload.ReviewRequestID, payload.RecipientID, payload.ChatID).Scan(&status, &boundMessageID, &text)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load retired Telegram review card: %w", err)
	}
	note := reviewClosureNote(status)
	if note == "" {
		return nil
	}
	// The stored text belongs to the message currently bound to the recipient.
	// A card that was superseded by a later message is stripped without it.
	if boundMessageID != payload.MessageID {
		text = ""
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

// ReviewCardLive reports whether a queued edit of a review card may still be
// applied. Once the request is terminal the card belongs to retirement, and an
// edit carrying its old keyboard must not bring the buttons back.
func (p *Processor) ReviewCardLive(ctx context.Context, reviewRequestID string) (bool, error) {
	var live bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request WHERE id=$1 AND status IN ('PENDING_SEND','OPEN'))`, reviewRequestID).Scan(&live); err != nil {
		return false, fmt.Errorf("load edited Telegram review request: %w", err)
	}
	return live, nil
}

// RecordReviewCardText keeps the stored card text in step with a successful
// edit, so a later retirement reproduces what Telegram shows. A message that is
// not a bound review card matches no row.
func (p *Processor) RecordReviewCardText(ctx context.Context, chatID, messageID int64, text string) error {
	_, err := p.pool.Exec(ctx, `UPDATE review_request_recipient SET delivered_text=NULLIF($3,'') WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, chatID, messageID, clean(text, 4000))
	return err
}

// BindMerchantLearningMessage records the separate merchant-learning question on
// the recipient row of the chat it was sent to, creating the row when the
// confirming chat never received the card.
func (p *Processor) BindMerchantLearningMessage(ctx context.Context, reviewRequestID string, chatID, messageID int64) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,merchant_learning_message_id) VALUES($1,$2,$3)
		ON CONFLICT (review_request_id,telegram_chat_id) DO UPDATE SET merchant_learning_message_id=EXCLUDED.merchant_learning_message_id`, reviewRequestID, chatID, messageID)
	if err != nil {
		return fmt.Errorf("bind merchant learning message: %w", err)
	}
	return nil
}
