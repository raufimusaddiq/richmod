package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
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
//
// The replying member's own copy is renewed with it (its seven-day window
// restarts), whether the whole projection expired or only that copy did.
func (p *Processor) renewExpiredReviewProjection(ctx context.Context, householdID string, update telegramUpdate) error {
	if _, err := p.pool.Exec(ctx, `UPDATE review_request_recipient rr SET delivery_status='SENT',card_expires_at=now()+interval '7 days'
		FROM review_request r,review_item ri,telegram_identity ti,household_member hm
		WHERE rr.review_request_id=r.id AND r.review_item_id=ri.id AND r.household_id=$1 AND ri.household_id=$1
		  AND ri.status IN ('OPEN','PENDING_SEND') AND r.status IN ('OPEN','EXPIRED')
		  AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND rr.delivery_status IN ('EXPIRED','SENT')
		  AND (rr.delivery_status='EXPIRED' OR r.status='EXPIRED' OR COALESCE(rr.card_expires_at,r.expires_at)<=now())
		  AND ti.telegram_user_id=$4 AND ti.household_id=$1 AND ti.active AND hm.household_id=$1 AND hm.user_id=ti.user_id AND hm.active`,
		householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID, update.Message.From.ID); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `UPDATE review_request r SET status='OPEN',expires_at=now()+interval '7 days' FROM review_item ri,review_request_recipient rr,telegram_identity ti,household_member hm WHERE r.review_item_id=ri.id AND rr.review_request_id=r.id AND ri.household_id=$1 AND r.household_id=$1 AND ri.status IN ('OPEN','PENDING_SEND') AND r.status IN ('OPEN','EXPIRED') AND (r.expires_at<=now() OR r.status='EXPIRED') AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND ti.telegram_user_id=$4 AND ti.household_id=$1 AND ti.active AND hm.household_id=$1 AND hm.user_id=ti.user_id AND hm.active`, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID, update.Message.From.ID)
	return err
}

// BindReviewMessage records the Telegram message a review card was delivered as,
// with the text Telegram shows, on the recipient row, and keeps every message of
// the review in review_request_message. The request row is locked first so a
// bind and a terminal transition serialize: either the transition commits first
// and the bind sees a closed request, or the bind commits first and the
// transition's retirement trigger sees the stored message. A card that arrives
// after its request closed is still recorded and queued for retirement, so it
// cannot stay tappable.
//
// A follow-up message (a chooser, a prompt for a typed value) becomes the
// recipient's current card. The message it replaces stays recorded as
// superseded and its buttons are removed now, so no older message of the review
// keeps live buttons that no longer bind.
func (p *Processor) BindReviewMessage(ctx context.Context, reviewRequestID string, chatID, messageID int64, text string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bind Telegram review message: %w", err)
	}
	defer tx.Rollback(ctx)
	var status, householdID string
	if err := tx.QueryRow(ctx, `SELECT status,household_id::text FROM review_request WHERE id=$1 FOR UPDATE`, reviewRequestID).Scan(&status, &householdID); err != nil {
		return fmt.Errorf("load Telegram review request after send: %w", err)
	}
	terminal := status == "RESOLVED" || status == "CANCELLED" || status == "EXPIRED"
	var recipientID string
	var previous int64
	err = tx.QueryRow(ctx, `SELECT id::text,COALESCE(telegram_message_id,0) FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2 FOR UPDATE`, reviewRequestID, chatID).Scan(&recipientID, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		if terminal {
			return nil
		}
		return fmt.Errorf("Telegram review request could not be bound")
	}
	if err != nil {
		return fmt.Errorf("bind Telegram review message: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request_recipient SET telegram_message_id=$2,delivered_text=NULLIF($3,''),delivery_status='SENT',queued_payload=NULL,delivery_error=NULL,delivery_failed_at=NULL,card_expires_at=COALESCE(card_expires_at,now()+interval '7 days') WHERE id=$1`,
		recipientID, messageID, clean(text, 4000)); err != nil {
		return fmt.Errorf("bind Telegram review message: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_request_message(household_id,review_request_id,recipient_id,telegram_chat_id,telegram_message_id,delivered_text)
		VALUES($1,$2,$3,$4,$5,NULLIF($6,''))
		ON CONFLICT (telegram_chat_id,telegram_message_id) DO UPDATE SET delivered_text=EXCLUDED.delivered_text,superseded_at=NULL`,
		householdID, reviewRequestID, recipientID, chatID, messageID, clean(text, 4000)); err != nil {
		return fmt.Errorf("record Telegram review message: %w", err)
	}
	if previous != 0 && previous != messageID {
		if _, err := tx.Exec(ctx, `UPDATE review_request_message SET superseded_at=now() WHERE recipient_id=$1 AND telegram_chat_id=$2 AND telegram_message_id=$3 AND superseded_at IS NULL`, recipientID, chatID, previous); err != nil {
			return fmt.Errorf("supersede Telegram review message: %w", err)
		}
		if !terminal {
			if err := enqueueSupersededCardRetirement(ctx, tx, reviewRequestID, recipientID, chatID, previous, status); err != nil {
				return err
			}
		}
	}
	switch {
	case terminal:
		if _, err := tx.Exec(ctx, `SELECT enqueue_review_card_retirement($1::uuid,$2::uuid)`, reviewRequestID, recipientID); err != nil {
			return fmt.Errorf("retire Telegram review card sent after close: %w", err)
		}
	case status == "PENDING_SEND":
		if _, err := tx.Exec(ctx, `UPDATE review_request SET status='OPEN' WHERE id=$1`, reviewRequestID); err != nil {
			return fmt.Errorf("open Telegram review request: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// enqueueSupersededCardRetirement removes the buttons of a review message that a
// newer message of the same review replaced in that chat.
func enqueueSupersededCardRetirement(ctx context.Context, tx pgx.Tx, reviewRequestID, recipientID string, chatID, messageID int64, status string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('RETIRE_TELEGRAM_REVIEW_CARD',jsonb_build_object('review_request_id',$1::text,'recipient_id',$2::text,'chat_id',$3::bigint,'message_id',$4::bigint,'status',$5::text,'superseded',true)) ON CONFLICT DO NOTHING`,
		reviewRequestID, recipientID, chatID, messageID, status)
	if err != nil {
		return fmt.Errorf("retire superseded Telegram review message: %w", err)
	}
	return nil
}

// CreateTransactionReviewItem creates the canonical active review_item for a
// NEEDS_REVIEW transaction with its ReviewDecision in the same INSERT and returns its id.
// status is "OPEN" (no Telegram delivery) or "PENDING_SEND" (a projection follows).
// decision overrides the preset for reviewType when provided; the stored decision must
// pass reviewdec validation (non-empty ReasonCode and AllowedActions) or an error is returned.
// Idempotent: if an active item already exists for the transaction, returns that item's id
// and does not overwrite it.
func CreateTransactionReviewItem(ctx context.Context, tx pgx.Tx, transactionID, reviewType, status string, decisions ...reviewdec.Decision) (string, error) {
	if status != "OPEN" && status != "PENDING_SEND" {
		return "", fmt.Errorf("review item status %q is not an active status", status)
	}
	var decision reviewdec.Decision
	if len(decisions) > 0 {
		decision = decisions[0]
	} else {
		var err error
		if decision, err = telegramReviewDecision(ctx, tx, transactionID, reviewType); err != nil {
			return "", err
		}
	}
	// JSON validates the contract, so an incomplete decision fails here instead
	// of being stored (the review_item trigger from 00078 refuses it as well).
	encoded, err := decision.JSON()
	if err != nil {
		return "", fmt.Errorf("%s review decision: %w", reviewType, err)
	}
	// review_item_active_transaction_unique allows one active item per
	// transaction; a retry or second producer reuses it untouched.
	var itemID string
	err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,preferred_user_id,decision)
		SELECT household_id,id,$2,$3,created_by_user_id,$4::jsonb FROM transaction WHERE id=$1
		ON CONFLICT (transaction_id) WHERE transaction_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN') DO NOTHING
		RETURNING id`, transactionID, reviewType, status, string(encoded)).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM review_item WHERE transaction_id=$1 AND status IN ('PENDING_SEND','OPEN')`, transactionID).Scan(&itemID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("review item for transaction %s: transaction not found", transactionID)
		}
	}
	if err != nil {
		return "", err
	}
	return itemID, nil
}

// EnqueueReviewRequest creates the canonical transaction review_item (through
// CreateTransactionReviewItem, so the decision is written with the item) and its
// Telegram review_request projection, then renders the card to recipients.
func EnqueueReviewRequest(ctx context.Context, tx pgx.Tx, transactionID, reviewType string, chatID, replyTo int64, message string, decisions ...reviewdec.Decision) error {
	itemID, err := CreateTransactionReviewItem(ctx, tx, transactionID, reviewType, "PENDING_SEND", decisions...)
	if err != nil {
		return err
	}
	// The projection renders the stored item, so a reused item is projected with
	// the type and contract it already carries, not a freshly computed one: one
	// transaction has one active review, and the first open question wins.
	var itemType string
	var decisionJSON []byte
	if err := tx.QueryRow(ctx, `SELECT review_type,decision FROM review_item WHERE id=$1`, itemID).Scan(&itemType, &decisionJSON); err != nil {
		return err
	}
	if itemType != reviewType {
		slog.Default().Warn("review projection reused an active item with a different review type", "transaction_id", transactionID, "review_item_id", itemID, "requested_type", reviewType, "stored_type", itemType)
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(decisionJSON, &decision); err != nil {
		return err
	}
	// One review_item has one Telegram projection (review_request_review_item_unique).
	// A producer that reuses an item which already has a request reuses that
	// request and its delivered cards instead of sending a second card; a request
	// that was created but never rendered is rendered now.
	var reviewID, requestStatus string
	var rendered bool
	err = tx.QueryRow(ctx, `SELECT r.id,r.status,EXISTS(SELECT 1 FROM review_conversation c WHERE c.review_request_id=r.id) FROM review_request r WHERE r.review_item_id=$1`, itemID).Scan(&reviewID, &requestStatus, &rendered)
	switch {
	case err == nil:
		if rendered || (requestStatus != "PENDING_SEND" && requestStatus != "OPEN") {
			return nil
		}
	case errors.Is(err, pgx.ErrNoRows):
		// reviewID is the review_request id (the handle every later enqueue uses);
		// itemID is the review_item row the ReviewDecision contract lives on.
		if err := tx.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,telegram_chat_id,status) SELECT id,household_id,transaction_id,review_type,NULLIF($2::bigint,0),'PENDING_SEND' FROM review_item WHERE id=$1 RETURNING id`, itemID, chatID).Scan(&reviewID); err != nil {
			return err
		}
	default:
		return err
	}
	return projectReviewRequest(ctx, tx, reviewID, itemID, itemType, decision, replyTo, message, chatID)
}

// projectReviewRequest is the one place a review_item becomes an actionable
// Telegram message. Every producer routes through it: the transaction
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
		if state == "AWAITING_MERCHANT" && reviewType == "UNKNOWN_MERCHANT" && decision.Subject.Type == "transaction" {
			markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Beli aset", CallbackData: "review:asset"}, {Text: "Abaikan", CallbackData: "review:ignore"}}}}
		}
	case "duplicate":
		if decision.Provenance != nil && decision.Provenance["evidence_link_dispute"] == true {
			markup = evidenceDisputeMarkup()
			break
		}
		if markup, err = projectDuplicateChoices(ctx, tx, reviewID); err != nil {
			return err
		}
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
	// Every active, authorized member receives the card so the whole household
	// knows about it. The member whose own message or upload raised the review
	// gets their copy threaded to it; the others get no reply thread (a message
	// id from another chat would point at an unrelated message) and a line naming
	// that member. A household-level source (bank or provider email, cycle
	// residual, wealth from email) has no originating member: no thread, no line.
	rows, err := tx.Query(ctx, `SELECT ti.telegram_user_id
		FROM telegram_identity ti
		JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
		JOIN "user" u ON u.id=ti.user_id AND u.active
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
			rows.Close()
			return err
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	// A producer that had no eligible recipient at creation time (no linked
	// Telegram identity, or a source-event review with no transaction) still
	// projects to the chat that originated it when one is supplied, but only
	// while that chat is an authorized member of the review's own household:
	// a member removed after uploading must not receive the card.
	if len(recipients) == 0 && originatingChatID != 0 {
		var reviewHousehold string
		if err := tx.QueryRow(ctx, `SELECT household_id::text FROM review_request WHERE id=$1`, reviewID).Scan(&reviewHousehold); err != nil {
			return err
		}
		authorized, err := telegramMemberAuthorized(ctx, tx, originatingChatID, reviewHousehold)
		if err != nil {
			return err
		}
		if !authorized {
			return nil
		}
		recipients = []int64{originatingChatID}
	}
	origin, err := reviewOrigin(ctx, tx, reviewID, originatingChatID, replyTo, recipients)
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		text, threadTo := reviewMessage, int64(0)
		if origin.chatID != 0 {
			if recipient == origin.chatID {
				threadTo = origin.messageID
			} else if origin.name != "" {
				text = reviewMessage + "\n\nDari " + origin.name
			}
		}
		if err := admitReviewCard(ctx, tx, reviewID, recipient, threadTo, text, markup); err != nil {
			return err
		}
	}
	return nil
}

// reviewCardOrigin is the member whose own Telegram message or upload raised a
// review: their chat, the message to thread their copy to, and the name other
// members' copies show. A zero chat means a household-level source.
type reviewCardOrigin struct {
	chatID    int64
	messageID int64
	name      string
}

// reviewOrigin decides who raised a review. A producer answering a member's
// message passes that chat and message. Otherwise the review's earliest source
// event decides: a Telegram upload names its chat and message; an email or a
// system source is household-level, even when the producer passed a member's
// chat only to have somewhere to deliver. The origin must be one of the
// recipients, so a removed member is never named.
func reviewOrigin(ctx context.Context, tx pgx.Tx, reviewID string, originatingChatID, replyTo int64, recipients []int64) (reviewCardOrigin, error) {
	origin := reviewCardOrigin{}
	if originatingChatID != 0 && replyTo != 0 {
		origin.chatID, origin.messageID = originatingChatID, replyTo
	} else {
		var sourceType string
		var sourceChat, sourceMessage int64
		err := tx.QueryRow(ctx, `SELECT s.source_type,COALESCE(s.telegram_chat_id,0),COALESCE(s.telegram_message_id,0)
			FROM review_request r
			JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
			LEFT JOIN document d ON d.id=ri.document_id AND d.household_id=ri.household_id
			LEFT JOIN transaction_proposal tp ON tp.id=ri.proposal_id
			JOIN source_event s ON s.household_id=ri.household_id AND (s.id=ri.source_event_id OR s.id=d.source_event_id OR s.id=tp.source_event_id
				OR s.id IN (SELECT te.source_event_id FROM transaction_evidence te WHERE te.transaction_id=ri.transaction_id))
			WHERE r.id=$1
			ORDER BY s.received_at,s.created_at,s.id LIMIT 1`, reviewID).Scan(&sourceType, &sourceChat, &sourceMessage)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return origin, err
		}
		if sourceType == "TELEGRAM_TEXT" || sourceType == "TELEGRAM_IMAGE" {
			origin.chatID, origin.messageID = sourceChat, sourceMessage
			if origin.chatID == 0 {
				origin.chatID, origin.messageID = originatingChatID, 0
			}
		}
	}
	if origin.chatID == 0 || !slices.Contains(recipients, origin.chatID) {
		return reviewCardOrigin{}, nil
	}
	var name string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(u.display_name,'') FROM review_request r
		JOIN telegram_identity ti ON ti.household_id=r.household_id AND ti.telegram_user_id=$2 AND ti.active
		JOIN "user" u ON u.id=ti.user_id
		WHERE r.id=$1`, reviewID, origin.chatID).Scan(&name); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return origin, err
	}
	origin.name = clean(name, 80)
	return origin, nil
}

// admitReviewCard records one recipient of a review card and hands its rendered
// send to admit_review_card, which sends it now or holds it QUEUED while the
// chat already shows three live review cards (migration 00090).
func admitReviewCard(ctx context.Context, tx pgx.Tx, reviewID string, chatID, replyTo int64, message string, markup *InlineKeyboardMarkup) error {
	// The chat's pacing lock comes before any recipient row (migration 00090).
	if _, err := tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, chatID); err != nil {
		return err
	}
	var recipientID string
	if err := tx.QueryRow(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2)
		ON CONFLICT (review_request_id,telegram_chat_id) DO UPDATE SET telegram_chat_id=EXCLUDED.telegram_chat_id RETURNING id`, reviewID, chatID).Scan(&recipientID); err != nil {
		return err
	}
	payload := map[string]any{"chat_id": chatID, "reply_to_message_id": replyTo, "text": clean(message, 4000), "review_request_id": reviewID}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var admitted string
	return tx.QueryRow(ctx, `SELECT admit_review_card($1::uuid,$2::jsonb)`, recipientID, string(encoded)).Scan(&admitted)
}

// ProjectReviewItem creates the single Telegram projection for an already
// inserted review_item, whatever its subject: transaction, document,
// proposal, source event, wealth observation, or financial email. Every
// producer calls this after it writes the item and its ReviewDecision. The
// request carries the item's transaction_id (when the subject has one) so the
// bound reply lane can join it. Idempotent: an existing open projection with a
// delivered message is reused; an existing request without a message is
// rendered now.
func ProjectReviewItem(ctx context.Context, tx pgx.Tx, householdID, itemID string, replyTo int64, message string, originatingChatID int64) error {
	var reviewID, reviewType, transactionID, documentID, status string
	var observationBacked bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,''),review_type,COALESCE(document_id::text,''),status,financial_email_observation_id IS NOT NULL FROM review_item WHERE id=$1 AND household_id=$2`, itemID, householdID).Scan(&transactionID, &reviewType, &documentID, &status, &observationBacked); err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return nil
	}
	// This observation resolver exists in Web, not the transaction-bound callbacks.
	if observationBacked && reviewType == "TRANSFER_CLASSIFICATION" && transactionID == "" {
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
	if err := tx.QueryRow(ctx, `SELECT decision FROM review_item WHERE id=$1`, itemID).Scan(&decisionJSON); err != nil {
		return err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(decisionJSON, &decision); err != nil {
		return err
	}
	// Document and email producers send no summary. Describe the transaction from
	// canonical state so the card says what it is about. The category chooser
	// sends its summary verbatim, so it keeps the decision prompt instead.
	if strings.TrimSpace(message) == "" && transactionID != "" && !isCategoryOnly(decision) {
		if message, err = transactionReviewSummary(ctx, tx, householdID, transactionID); err != nil {
			return err
		}
	}
	return projectReviewRequest(ctx, tx, reviewID, itemID, reviewType, decision, replyTo, message, originatingChatID)
}

// transactionReviewSummary names a reviewed transaction by its amount and its
// merchant (or description when no merchant is known).
func transactionReviewSummary(ctx context.Context, tx pgx.Tx, householdID, transactionID string) (string, error) {
	var amount, merchant string
	err := tx.QueryRow(ctx, `SELECT round(t.amount)::text,COALESCE(NULLIF(m.normalized_name,''),NULLIF(t.description,''),'')
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id
		WHERE t.id=$1 AND t.household_id=$2`, transactionID, householdID).Scan(&amount, &merchant)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	summary := "Nominal: Rp" + FormatIDR(amount)
	if merchant != "" {
		summary += "\nMerchant: " + merchant
	}
	return summary, nil
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
// cannot finish the work. The producers call this before projecting; a family
// is enabled here once Telegram can resolve it.
