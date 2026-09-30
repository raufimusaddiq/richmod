package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

func (p *Processor) ReviewProjectionOpen(ctx context.Context, reviewRequestID string) (bool, error) {
	if reviewRequestID == "" {
		return true, nil
	}
	var open bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request WHERE id=$1 AND status IN ('PENDING_SEND','OPEN') AND expires_at>now())`, reviewRequestID).Scan(&open); err != nil {
		return false, err
	}
	return open, nil
}

// A request TTL only bounds the Telegram projection, never the canonical item.
// A member replying to its exact old card renews that same projection; no new
// review_item is created and all existing callback binding remains server-owned.
func (p *Processor) renewExpiredReviewProjection(ctx context.Context, householdID string, update telegramUpdate) error {
	_, err := p.pool.Exec(ctx, `UPDATE review_request r SET status='OPEN',expires_at=now()+interval '7 days' FROM review_item ri,review_request_recipient rr,telegram_identity ti,household_member hm WHERE r.review_item_id=ri.id AND rr.review_request_id=r.id AND ri.household_id=$1 AND r.household_id=$1 AND ri.status IN ('OPEN','PENDING_SEND') AND r.status IN ('OPEN','EXPIRED') AND (r.expires_at<=now() OR r.status='EXPIRED') AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND ti.telegram_user_id=$4 AND ti.household_id=$1 AND ti.active AND hm.household_id=$1 AND hm.user_id=ti.user_id AND hm.active`, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID, update.Message.From.ID)
	return err
}

func (p *Processor) BindReviewMessage(ctx context.Context, reviewRequestID string, chatID, messageID int64) error {
	result, err := p.pool.Exec(ctx, `
		UPDATE review_request_recipient rr
		SET telegram_message_id=$3
		FROM review_request r
		WHERE rr.review_request_id=$1 AND rr.telegram_chat_id=$2
		  AND r.id=rr.review_request_id AND r.status IN ('PENDING_SEND','OPEN') AND r.expires_at>now()`,
		reviewRequestID, chatID, messageID)
	if err != nil {
		return fmt.Errorf("bind Telegram review message: %w", err)
	}
	if result.RowsAffected() == 1 {
		if _, err := p.pool.Exec(ctx, `UPDATE review_request SET status='OPEN' WHERE id=$1 AND status='PENDING_SEND'`, reviewRequestID); err != nil {
			return fmt.Errorf("open Telegram review request: %w", err)
		}
	}
	if result.RowsAffected() != 1 {
		var status string
		if err := p.pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, reviewRequestID).Scan(&status); err != nil {
			return fmt.Errorf("load Telegram review request after send: %w", err)
		}
		if status == "RESOLVED" || status == "CANCELLED" || status == "EXPIRED" {
			return nil
		}
		return fmt.Errorf("Telegram review request could not be bound")
	}
	return nil
}

// completeBankFactsReply resolves a UNKNOWN_BANK_TEMPLATE review from Telegram.
// It parses the amount and timestamp out of the bound reply, validates the
// account and completes through the shared operation, then re-runs the same
// deterministic bank policy locally and hands a completed transaction to the
// shared confirm path. A reply that leaves a required fact missing re-asks for it
// instead of guessing.
func EnqueueReviewRequest(ctx context.Context, tx pgx.Tx, transactionID, reviewType string, chatID, replyTo int64, message string, decisions ...reviewdec.Decision) error {
	// reviewID is the review_request id (the handle every later enqueue uses);
	// itemID is the review_item row the ReviewDecision contract lives on. They are
	// different rows, so the decision write must target itemID or it silently
	// updates nothing.
	var reviewID, itemID string
	err := tx.QueryRow(ctx, `WITH item AS (INSERT INTO review_item(household_id,transaction_id,review_type,status,preferred_user_id) SELECT household_id,id,$2,'PENDING_SEND',created_by_user_id FROM transaction WHERE id=$1 RETURNING id,household_id,transaction_id) INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,telegram_chat_id,status) SELECT item.id,item.household_id,item.transaction_id,$2,$3,'PENDING_SEND' FROM item RETURNING id,(SELECT id FROM item)`, transactionID, reviewType, chatID).Scan(&reviewID, &itemID)
	if err != nil {
		return err
	}
	// PRD 7/37: every Telegram review carries the same ReviewDecision contract
	// the Inbox renders, written at the one place all reviews are created, so a
	// Telegram review and a web review ask for exactly the same unresolved fact.
	decision, err := telegramReviewDecision(ctx, tx, transactionID, reviewType)
	if err != nil {
		return err
	}
	if len(decisions) > 0 {
		decision = decisions[0]
	}
	if decision.ReasonCode != "" {
		encoded, err := decision.JSON()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_item SET decision=$2::jsonb,updated_at=now() WHERE id=$1`, itemID, string(encoded)); err != nil {
			return err
		}
	}
	return projectReviewRequest(ctx, tx, reviewID, itemID, reviewType, decision, replyTo, message, chatID)
}

// projectReviewRequest is the one place a review_item becomes an actionable
// Telegram message (UIR-02). Every producer routes through it: the transaction
// adapter creates the item first, the source/document adapter inserts a
// source-event item, and both then call this with the same decision-driven
// renderer. A review that has no eligible recipient produces no projection.
// That keeps a non-transaction review (source, document, wealth, cycle) off the
// transaction-only bound path while sharing all delivery, recipient, and
// markup logic.
func projectReviewRequest(ctx context.Context, tx pgx.Tx, reviewID, itemID, reviewType string, decision reviewdec.Decision, replyTo int64, message string, originatingChatID int64) error {
	state, reviewMessage, markupMode := renderReviewPresentation(decision, reviewType, message)
	if _, err := tx.Exec(ctx, `INSERT INTO review_conversation (review_request_id,state) VALUES ($1,$2)`, reviewID, state); err != nil {
		return err
	}
	var markup *InlineKeyboardMarkup
	var err error
	switch markupMode {
	case "category", "transfer":
		markup = reviewActionMarkupPage(ctx, tx, reviewID, reviewType, 0)
	case "reply":
		markup = requiredFieldReplyMarkup()
		if state == "AWAITING_MERCHANT" {
			// The original category-first card offered Beli aset here; a merchant-first
			// review must not lose that escape hatch (ADR-036).
			markup = merchantReviewMarkup()
		}
	case "duplicate":
		markup = duplicateIntentMarkup()
	case "salary":
		markup = salaryPolicyMarkup(decision.AllowedActions)
	case "document":
		markup = documentReviewMarkup()
	case "financial_email":
		markup, err = financialEmailEntityMarkup(ctx, tx, reviewID, decision, 0)
		if err != nil {
			return err
		}
	case "receipt_quality":
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Terima total", CallbackData: "review:quality:confirm"}, {Text: "Abaikan", CallbackData: "review:ignore"}}}}
	case "financial_email_facts":
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: "review:ignore"}}}}
	default:
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ubah detail", CallbackData: "review:edit"}, {Text: "Abaikan", CallbackData: "review:ignore"}}}}
	}
	rows, err := tx.Query(ctx, `SELECT ti.telegram_user_id
		FROM telegram_identity ti
		JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
		JOIN review_request r ON r.household_id=ti.household_id AND r.id=$1
		LEFT JOIN review_item ri ON ri.id=r.review_item_id
		WHERE ti.active
		ORDER BY CASE WHEN ti.user_id=ri.preferred_user_id THEN 0 WHEN hm.role='OWNER' THEN 1 ELSE 2 END, ti.created_at`, reviewID)
	if err != nil {
		return err
	}
	var recipients []int64
	for rows.Next() {
		var recipient int64
		if err := rows.Scan(&recipient); err != nil {
			return err
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, recipient := range recipients {
		if _, err := tx.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, reviewID, recipient); err != nil {
			return err
		}
		if err := enqueueReviewMessageWithMarkup(ctx, tx, reviewID, recipient, replyTo, reviewMessage, markup); err != nil {
			return err
		}
	}
	// A producer that had no eligible recipient at creation time (no linked
	// Telegram identity, or a source-event review with no transaction) still
	// projects to the chat that originated it when one is supplied.
	if len(recipients) == 0 && originatingChatID != 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, reviewID, originatingChatID); err != nil {
			return err
		}
		if err := enqueueReviewMessageWithMarkup(ctx, tx, reviewID, originatingChatID, replyTo, reviewMessage, markup); err != nil {
			return err
		}
	}
	return nil
}

// ProjectReviewItem creates the single Telegram projection for an already
// inserted review_item (UIR-02), whatever its subject: transaction, document,
// proposal, source event, wealth observation, or financial email. Every
// producer calls this after it writes the item and its ReviewDecision. The
// request carries the item's transaction_id (when the subject has one) so the
// bound reply lane can join it. Idempotent: an existing open projection with a
// delivered message is reused; an existing request without a message is
// rendered now.
func ProjectReviewItem(ctx context.Context, tx pgx.Tx, householdID, itemID string, replyTo int64, message string, originatingChatID int64) error {
	var reviewID, reviewType, transactionID, documentID, status string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,''),review_type,COALESCE(document_id::text,''),status FROM review_item WHERE id=$1 AND household_id=$2`, itemID, householdID).Scan(&transactionID, &reviewType, &documentID, &status); err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return nil
	}
	// The document action handler requires an actual document binding. Bank
	// emails may reuse the extraction-failure reason but have no document;
	// those reviews remain actionable in the Web Inbox, not in Telegram.
	if (reviewType == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" || reviewType == "DOCUMENT_CLASSIFICATION") && documentID == "" {
		return nil
	}
	// Fail closed: never send a card whose buttons cannot complete the review.
	if !TelegramCompletableReviewType(reviewType) {
		return nil
	}
	// Reuse an open request for this item; only render it when it has no bound
	// message yet, otherwise a re-run would double-send the card.
	// A projection is "rendered" once its conversation row exists. The send job
	// binds the Telegram message id later, so checking only the recipient would
	// re-render (and re-insert review_conversation) on a second call.
	var alreadyRendered bool
	err := tx.QueryRow(ctx, `SELECT r.id, EXISTS(SELECT 1 FROM review_conversation c WHERE c.review_request_id=r.id)
		FROM review_request r WHERE r.review_item_id=$1 AND r.status IN ('PENDING_SEND','OPEN') ORDER BY r.created_at LIMIT 1`, itemID).Scan(&reviewID, &alreadyRendered)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,status)
			VALUES($1,$2,NULLIF($3,'')::uuid,$4,'OPEN') RETURNING id`, itemID, householdID, transactionID, reviewType).Scan(&reviewID)
	}
	if err != nil {
		return err
	}
	if alreadyRendered {
		return nil
	}
	var decisionJSON []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(decision,'{}'::jsonb) FROM review_item WHERE id=$1`, itemID).Scan(&decisionJSON); err != nil {
		return err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(decisionJSON, &decision); err != nil {
		return err
	}
	return projectReviewRequest(ctx, tx, reviewID, itemID, reviewType, decision, replyTo, message, originatingChatID)
}

// ProjectReviewMessage is the message-free convenience for producers that do
// not supply a bespoke prompt: the renderer derives one from the decision.
func ProjectReviewMessage(ctx context.Context, tx pgx.Tx, householdID, itemID string, chatID, replyTo int64) error {
	return ProjectReviewItem(ctx, tx, householdID, itemID, replyTo, "", chatID)
}

// TelegramCompletableReviewType reports whether a review subject can be resolved
// to completion from Telegram today. A review whose completion path still lives
// only in the Review Inbox (document classification, bank-fact completion,
// payslip policy) must not be projected, or the user gets a card whose buttons
// cannot finish the work. The producers call this before projecting; the family
// is enabled here when its UIR-06/UIR-07 resolver lands.
