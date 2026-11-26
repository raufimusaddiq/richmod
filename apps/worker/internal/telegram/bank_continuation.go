package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

type cardCopy struct {
	RecipientID string
	ChatID      int64
	MessageID   int64
}

type bankCompletion struct {
	Status        string
	Linked        bool
	TransactionID string
	Amount        string
	At            time.Time
	Merchant      string
	Category      string
}

func loadCardCopies(ctx context.Context, tx pgx.Tx, itemID string) (string, string, []cardCopy, error) {
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.household_id::text,rr.id::text,rr.telegram_chat_id,rr.telegram_message_id
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.review_item_id=$1 AND rr.telegram_message_id IS NOT NULL AND rr.delivery_status='SENT'
		ORDER BY r.created_at,rr.created_at`, itemID)
	if err != nil {
		return "", "", nil, err
	}
	defer rows.Close()
	var requestID, householdID string
	var copies []cardCopy
	for rows.Next() {
		var copy cardCopy
		if err := rows.Scan(&requestID, &householdID, &copy.RecipientID, &copy.ChatID, &copy.MessageID); err != nil {
			return "", "", nil, err
		}
		copies = append(copies, copy)
	}
	return requestID, householdID, copies, rows.Err()
}

func BankCardContinuable(ctx context.Context, tx pgx.Tx, reviewItemID string) (bool, error) {
	_, _, copies, err := loadCardCopies(ctx, tx, reviewItemID)
	return len(copies) > 0, err
}

func loadBankCompletion(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string) (bankCompletion, error) {
	var out bankCompletion
	var matched string
	err := tx.QueryRow(ctx, `SELECT p.proposal_status,COALESCE(p.metadata_json->>'matched_transaction_id',''),COALESCE(t.id::text,''),COALESCE(t.status,''),
			COALESCE(round(p.amount)::text,''),p.transaction_at,COALESCE(m.normalized_name,''),COALESCE(c.name,'')
		FROM transaction_proposal p
		LEFT JOIN transaction_evidence te ON te.source_event_id=p.source_event_id AND te.evidence_type='BANK_EMAIL' AND te.metadata_json->>'proposal_id'=p.id::text
		LEFT JOIN transaction t ON t.id=te.transaction_id AND t.household_id=p.household_id
		LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
		LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id
		WHERE p.source_event_id=$1 AND p.household_id=$2 ORDER BY p.created_at DESC LIMIT 1`, sourceEventID, householdID).
		Scan(&out.Status, &matched, &out.TransactionID, new(string), &out.Amount, &out.At, &out.Merchant, &out.Category)
	if errors.Is(err, pgx.ErrNoRows) {
		return bankCompletion{}, nil
	}
	if err != nil {
		return bankCompletion{}, err
	}
	out.Linked = matched != ""
	if out.TransactionID != "" {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1 AND household_id=$2`, out.TransactionID, householdID).Scan(&status); err != nil {
			return bankCompletion{}, err
		}
		out.Status = status
		if out.Linked {
			out.Status = "LINKED"
		}
	}
	return out, nil
}

func bankDoneLine(done bankCompletion) string {
	switch {
	case done.Amount == "":
		return "✅ Email bank diproses tanpa mencatat transaksi."
	case done.Linked:
		return "✅ Digabung dengan transaksi yang sudah ada: " + joinDot(idr(done.Amount), bankInstantLabel(done.At)) + "."
	}
	return savedLine(done.Merchant, done.Category, idr(done.Amount), bankInstantLabel(done.At))
}

func bankInstantLabel(at time.Time) string {
	local := at.In(jakartaLocation())
	return reviewdomain.FormatIndonesianShortDate(local) + " " + local.Format("15:04")
}

func ContinueBankCard(ctx context.Context, tx pgx.Tx, reviewItemID string) error {
	requestID, householdID, copies, err := loadCardCopies(ctx, tx, reviewItemID)
	if err != nil || len(copies) == 0 {
		return err
	}
	var sourceEventID string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(source_event_id::text,'') FROM review_item WHERE id=$1 AND household_id=$2`, reviewItemID, householdID).Scan(&sourceEventID); err != nil {
		return err
	}
	done, err := loadBankCompletion(ctx, tx, householdID, sourceEventID)
	if err != nil {
		return err
	}
	if done.Status == "NEEDS_REVIEW" {
		continued, err := continueIntoReview(ctx, tx, householdID, requestID, done.TransactionID, copies)
		if err != nil || continued {
			return err
		}
		done.Status = "CONFIRMED"
	}
	text := bankDoneLine(done)
	for _, copy := range copies {
		if err := settleReviewCardInPlace(ctx, tx, requestID, copy.ChatID, copy.MessageID, text, nil); err != nil {
			return err
		}
	}
	return nil
}

func continueIntoReview(ctx context.Context, tx pgx.Tx, householdID, oldRequestID, transactionID string, copies []cardCopy) (bool, error) {
	var itemID, reviewType string
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT id::text,review_type,decision FROM review_item WHERE transaction_id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, transactionID, householdID).Scan(&itemID, &reviewType, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var existing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request WHERE review_item_id=$1)`, itemID).Scan(&existing); err != nil {
		return false, err
	}
	if existing || !reviewdec.TelegramProjectable(reviewType) {
		return false, nil
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return false, err
	}
	var requestID string
	if err := tx.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,status) VALUES($1,$2,$3,$4,'OPEN') RETURNING id::text`, itemID, householdID, transactionID, reviewType).Scan(&requestID); err != nil {
		return false, err
	}
	facts, err := reviewItemSummary(ctx, tx, householdID, itemID)
	if err != nil {
		return false, err
	}
	subject, err := loadReviewSubject(ctx, tx, requestID)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,$2)`, requestID, buildReviewCard(decision, reviewType, facts, subject).State); err != nil {
		return false, err
	}
	card, markup, err := renderStepCard(ctx, tx, requestID, reviewType, facts, decision, subject)
	if err != nil {
		return false, err
	}
	return true, moveCardsToRequest(ctx, tx, oldRequestID, requestID, copies, card.Text(), markup)
}

func moveCardsToRequest(ctx context.Context, tx pgx.Tx, oldRequestID, requestID string, copies []cardCopy, cardText string, markup *InlineKeyboardMarkup) error {
	if markup == nil {
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	encodedMarkup, err := json.Marshal(markup)
	if err != nil {
		return err
	}
	text := clean(cardText, 4000)
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='OPEN',expires_at=GREATEST(expires_at,now()+interval '7 days') WHERE id=$1 AND status='PENDING_SEND'`, requestID); err != nil {
		return err
	}
	for _, copy := range copies {
		if _, err := tx.Exec(ctx, `UPDATE review_request_recipient SET telegram_message_id=NULL,delivery_status='SKIPPED' WHERE id=$1`, copy.RecipientID); err != nil {
			return err
		}
		var recipientID string
		if err := tx.QueryRow(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id,delivery_status,delivered_text,card_expires_at) VALUES($1,$2,$3,'SENT',$4,now()+interval '7 days') RETURNING id::text`, requestID, copy.ChatID, copy.MessageID, text).Scan(&recipientID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_request_message SET review_request_id=$3,recipient_id=$4,delivered_text=$5,superseded_at=NULL WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, copy.ChatID, copy.MessageID, requestID, recipientID, text); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE job SET status='SUCCEEDED',finished_at=now(),updated_at=now(),payload_json=payload_json||'{"superseded_by":"card_continuation"}'::jsonb WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND status='PENDING' AND payload_json->>'review_request_id'=$1 AND (payload_json->>'chat_id')::bigint=$2 AND (payload_json->>'message_id')::bigint=$3`, oldRequestID, copy.ChatID, copy.MessageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json) VALUES('EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('chat_id',$1::bigint,'message_id',$2::bigint,'text',$3::text,'reply_markup',$4::jsonb,'review_request_id',$5::text))`, copy.ChatID, copy.MessageID, text, string(encodedMarkup), requestID); err != nil {
			return err
		}
	}
	return nil
}
