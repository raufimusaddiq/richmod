package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

type RetireCardPayload struct {
	ReviewRequestID string `json:"review_request_id"`
	RecipientID     string `json:"recipient_id"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int64  `json:"message_id"`
	Status          string `json:"status"`
	Superseded      bool   `json:"superseded,omitempty"`
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
	if status == "RESOLVED" {
		closing, err := p.loadClosingNote(ctx, payload, boundMessageID)
		if err != nil {
			return err
		}
		if closing != "" {
			note = closing
		}
	}
	switch {
	case note != "":
	case recipientStatus == "EXPIRED":
		note = reviewClosureNote("EXPIRED", "")
	case payload.Superseded && boundMessageID != payload.MessageID:
		note = supersededCardNote
	default:
		return nil
	}
	var settled bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_conversation WHERE review_request_id=$1 AND COALESCE(context_json->'card_outcomes','{}'::jsonb) ? $2)`, payload.ReviewRequestID, cardOutcomeKey(payload.ChatID, payload.MessageID)).Scan(&settled); err != nil {
		return fmt.Errorf("load settled Telegram review card: %w", err)
	}
	if settled {
		return nil
	}
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

func (p *Processor) ReviewCardLive(ctx context.Context, reviewRequestID string) (bool, error) {
	var live bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request WHERE id=$1 AND status IN ('PENDING_SEND','OPEN'))`, reviewRequestID).Scan(&live); err != nil {
		return false, fmt.Errorf("load edited Telegram review request: %w", err)
	}
	return live, nil
}

func (p *Processor) RecordReviewCardText(ctx context.Context, chatID, messageID int64, text string) error {
	if _, err := p.pool.Exec(ctx, `UPDATE review_request_recipient SET delivered_text=NULLIF($3,'') WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, chatID, messageID, clean(text, 4000)); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `UPDATE review_request_message SET delivered_text=NULLIF($3,'') WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, chatID, messageID, clean(text, 4000))
	return err
}

func (p *Processor) BindMerchantLearningMessage(ctx context.Context, reviewRequestID string, chatID, messageID int64) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,merchant_learning_message_id,delivery_status) VALUES($1,$2,$3,'SKIPPED')
		ON CONFLICT (review_request_id,telegram_chat_id) DO UPDATE SET merchant_learning_message_id=EXCLUDED.merchant_learning_message_id`, reviewRequestID, chatID, messageID)
	if err != nil {
		return fmt.Errorf("bind merchant learning message: %w", err)
	}
	return nil
}

func capitalizeFirst(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func closingNoteText(via, resolverName, doneLine, outcomePhrase string) string {
	var head string
	switch via {
	case "TELEGRAM":
		head = "Sudah diselesaikan oleh " + resolverName + " di Telegram."
	case "WEB":
		head = "Sudah diselesaikan oleh " + resolverName + " di aplikasi."
	default:
		head = "Sudah diselesaikan otomatis."
	}
	summary := strings.TrimSpace(doneLine)
	if summary == "" && outcomePhrase != "" && via != "SYSTEM" {
		summary = capitalizeFirst(outcomePhrase) + "."
	}
	if summary == "" {
		return head
	}
	return head + "\n" + summary
}

func (p *Processor) loadClosingNote(ctx context.Context, payload RetireCardPayload, boundMessageID int64) (string, error) {
	var via, resolverID, resolverName, doneLine, recipientUser, action string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(ri.resolution_action,''),COALESCE(ri.resolved_via,''),COALESCE(ri.resolved_by_user_id::text,''),COALESCE(u.display_name,''),
			COALESCE(c.context_json->>'done_line',''),COALESCE(ti.user_id::text,'')
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		LEFT JOIN "user" u ON u.id=ri.resolved_by_user_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		LEFT JOIN telegram_identity ti ON ti.telegram_user_id=$2 AND ti.household_id=r.household_id
		WHERE r.id=$1`, payload.ReviewRequestID, payload.ChatID).Scan(&action, &via, &resolverID, &resolverName, &doneLine, &recipientUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load Telegram review closing note: %w", err)
	}
	if via == "" {
		return "", nil
	}
	if via == "TELEGRAM" && resolverID != "" && resolverID == recipientUser && boundMessageID != payload.MessageID {
		return supersededCardNote, nil
	}
	resolverName = clean(resolverName, 80)
	if via != "SYSTEM" && resolverName == "" {
		via = "SYSTEM"
	}
	phrase := ""
	if !autoClosedAction(action) {
		var err error
		if phrase, err = reviewOutcomeFor(ctx, p.pool, payload.ReviewRequestID, false); err != nil {
			return "", fmt.Errorf("load Telegram review closing summary: %w", err)
		}
	}
	return closingNoteText(via, resolverName, doneLine, phrase), nil
}
