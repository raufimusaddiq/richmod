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

func (p *Processor) renewExpiredReviewProjection(ctx context.Context, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var requestID, recipientID string
	err = tx.QueryRow(ctx, `SELECT r.id::text,rr.id::text
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=r.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND ri.status IN ('OPEN','PENDING_SEND') AND r.status IN ('OPEN','EXPIRED')
		  AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		FOR UPDATE OF r`, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID, update.Message.From.ID).Scan(&requestID, &recipientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, update.Message.Chat.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request_recipient rr SET delivery_status='SENT',card_expires_at=now()+interval '7 days'
		FROM review_request r
		WHERE rr.id=$1 AND r.id=rr.review_request_id AND rr.delivery_status IN ('EXPIRED','SENT')
		  AND (rr.delivery_status='EXPIRED' OR r.status='EXPIRED' OR COALESCE(rr.card_expires_at,r.expires_at)<=now())`, recipientID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='OPEN',expires_at=GREATEST(expires_at,now()+interval '7 days') WHERE id=$1 AND (status='EXPIRED' OR expires_at<=now())`, requestID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

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
	var recipientID, deliveryStatus string
	var previous int64
	err = tx.QueryRow(ctx, `SELECT id::text,COALESCE(telegram_message_id,0),delivery_status FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2 FOR UPDATE`, reviewRequestID, chatID).Scan(&recipientID, &previous, &deliveryStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		if terminal {
			return nil
		}
		return fmt.Errorf("Telegram review request could not be bound")
	}
	if err != nil {
		return fmt.Errorf("bind Telegram review message: %w", err)
	}
	firstDelivery := deliveryStatus != "SENT" && deliveryStatus != "EXPIRED"
	expiredCopy := deliveryStatus == "EXPIRED"
	if _, err := tx.Exec(ctx, `UPDATE review_request_recipient SET telegram_message_id=$2,delivered_text=NULLIF($3,''),delivery_status=CASE WHEN delivery_status='EXPIRED' THEN 'EXPIRED' ELSE 'SENT' END,queued_payload=NULL,delivery_error=NULL,delivery_failed_at=NULL,
		card_expires_at=CASE WHEN $4 OR card_expires_at IS NULL THEN now()+interval '7 days' ELSE card_expires_at END WHERE id=$1`,
		recipientID, messageID, clean(text, 4000), firstDelivery); err != nil {
		return fmt.Errorf("bind Telegram review message: %w", err)
	}
	if firstDelivery && !terminal {
		if _, err := tx.Exec(ctx, `UPDATE review_request SET expires_at=GREATEST(expires_at,now()+interval '7 days') WHERE id=$1`, reviewRequestID); err != nil {
			return fmt.Errorf("extend Telegram review projection: %w", err)
		}
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
	if expiredCopy && !terminal {
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('RETIRE_TELEGRAM_REVIEW_CARD',jsonb_build_object('review_request_id',$1::text,'recipient_id',$2::text,'chat_id',$3::bigint,'message_id',$4::bigint,'status','EXPIRED')) ON CONFLICT DO NOTHING`,
			reviewRequestID, recipientID, chatID, messageID); err != nil {
			return fmt.Errorf("retire Telegram review message sent after its copy expired: %w", err)
		}
	}
	if previous != messageID {
		recordStepShown(ctx, tx, reviewRequestID)
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

func recordStepShown(ctx context.Context, tx pgx.Tx, reviewRequestID string) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	if _, err := savepoint.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,transaction_id,review_item_id,event_type,action)
		SELECT household_id,transaction_id,review_item_id,'INTERACTION','STEP_SHOWN' FROM review_request WHERE id=$1`, reviewRequestID); err != nil {
		_ = savepoint.Rollback(ctx)
		slog.Default().Warn("record step shown telemetry failed", "review_request_id", reviewRequestID, "error", err)
		return
	}
	_ = savepoint.Commit(ctx)
}

func enqueueSupersededCardRetirement(ctx context.Context, tx pgx.Tx, reviewRequestID, recipientID string, chatID, messageID int64, status string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('RETIRE_TELEGRAM_REVIEW_CARD',jsonb_build_object('review_request_id',$1::text,'recipient_id',$2::text,'chat_id',$3::bigint,'message_id',$4::bigint,'status',$5::text,'superseded',true)) ON CONFLICT DO NOTHING`,
		reviewRequestID, recipientID, chatID, messageID, status)
	if err != nil {
		return fmt.Errorf("retire superseded Telegram review message: %w", err)
	}
	return nil
}

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
	encoded, err := decision.JSON()
	if err != nil {
		return "", fmt.Errorf("%s review decision: %w", reviewType, err)
	}
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

func EnqueueReviewRequest(ctx context.Context, tx pgx.Tx, transactionID, reviewType string, chatID, replyTo int64, message string, decisions ...reviewdec.Decision) error {
	itemID, err := CreateTransactionReviewItem(ctx, tx, transactionID, reviewType, "PENDING_SEND", decisions...)
	if err != nil {
		return err
	}
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
	var reviewID, requestStatus string
	var rendered bool
	err = tx.QueryRow(ctx, `SELECT r.id,r.status,EXISTS(SELECT 1 FROM review_conversation c WHERE c.review_request_id=r.id) FROM review_request r WHERE r.review_item_id=$1`, itemID).Scan(&reviewID, &requestStatus, &rendered)
	switch {
	case err == nil:
		if rendered || (requestStatus != "PENDING_SEND" && requestStatus != "OPEN") {
			return nil
		}
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,telegram_chat_id,status) SELECT id,household_id,transaction_id,review_type,NULLIF($2::bigint,0),'PENDING_SEND' FROM review_item WHERE id=$1 RETURNING id`, itemID, chatID).Scan(&reviewID); err != nil {
			return err
		}
	default:
		return err
	}
	return projectReviewRequest(ctx, tx, reviewID, itemID, itemType, decision, replyTo, message, chatID)
}

func projectReviewRequest(ctx context.Context, tx pgx.Tx, reviewID, itemID, reviewType string, decision reviewdec.Decision, replyTo int64, message string, originatingChatID int64) error {
	subject, err := loadReviewSubject(ctx, tx, reviewID)
	if err != nil {
		return err
	}
	card := buildReviewCard(decision, reviewType, message, subject)
	state, reviewMessage := card.State, card.Text()
	if _, err := tx.Exec(ctx, `INSERT INTO review_conversation (review_request_id,state) VALUES ($1,$2)`, reviewID, state); err != nil {
		return err
	}
	markup, err := reviewCardMarkup(ctx, tx, reviewID, reviewType, decision, card, subject)
	if err != nil {
		return err
	}
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
	if err := lockReviewCardChats(ctx, tx, recipients); err != nil {
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

func lockReviewCardChats(ctx context.Context, tx pgx.Tx, chats []int64) error {
	sorted := slices.Clone(chats)
	slices.Sort(sorted)
	for _, chat := range slices.Compact(sorted) {
		if _, err := tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, chat); err != nil {
			return err
		}
	}
	return nil
}

type reviewCardOrigin struct {
	chatID    int64
	messageID int64
	name      string
}

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

func admitReviewCard(ctx context.Context, tx pgx.Tx, reviewID string, chatID, replyTo int64, message string, markup *InlineKeyboardMarkup) error {
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

func ProjectReviewItem(ctx context.Context, tx pgx.Tx, householdID, itemID string, replyTo int64, message string, originatingChatID int64) error {
	var reviewID, reviewType, transactionID, documentID, status string
	var observationBacked bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,''),review_type,COALESCE(document_id::text,''),status,financial_email_observation_id IS NOT NULL FROM review_item WHERE id=$1 AND household_id=$2`, itemID, householdID).Scan(&transactionID, &reviewType, &documentID, &status, &observationBacked); err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return nil
	}
	if observationBacked && reviewType == "TRANSFER_CLASSIFICATION" && transactionID == "" {
		return nil
	}
	if (reviewType == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" || reviewType == "DOCUMENT_CLASSIFICATION") && documentID == "" {
		return nil
	}
	if !TelegramCompletableReviewType(reviewType) {
		return nil
	}
	var alreadyRendered bool
	var requestStatus string
	err := tx.QueryRow(ctx, `SELECT r.id, r.status, EXISTS(SELECT 1 FROM review_conversation c WHERE c.review_request_id=r.id)
		FROM review_request r WHERE r.review_item_id=$1 ORDER BY r.created_at LIMIT 1`, itemID).Scan(&reviewID, &requestStatus, &alreadyRendered)
	if err == nil && requestStatus != "PENDING_SEND" && requestStatus != "OPEN" {
		slog.Default().Info("review not projected: its request is already closed", "review_item_id", itemID, "review_request_id", reviewID, "request_status", requestStatus)
		return nil
	}
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
	if strings.TrimSpace(message) == "" {
		if message, err = reviewItemSummary(ctx, tx, householdID, itemID); err != nil {
			return err
		}
	}
	return projectReviewRequest(ctx, tx, reviewID, itemID, reviewType, decision, replyTo, message, originatingChatID)
}

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

func ProjectReviewMessage(ctx context.Context, tx pgx.Tx, householdID, itemID string, chatID, replyTo int64) error {
	return ProjectReviewItem(ctx, tx, householdID, itemID, replyTo, "", chatID)
}
