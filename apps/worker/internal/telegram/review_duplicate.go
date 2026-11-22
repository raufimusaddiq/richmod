package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func duplicateIntentMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Catat sebagai baru", CallbackData: "review:dup:new"}, {Text: "Abaikan", CallbackData: "review:ignore"}}}}
}

func reviewDetailMarkup() *InlineKeyboardMarkup {
	keyboard := [][]InlineKeyboardButton{{{Text: "Nama merchant", CallbackData: "review:merchant"}, {Text: "Keterangan", CallbackData: "review:description"}}}
	keyboard = append(keyboard, []InlineKeyboardButton{{Text: "Kategori", CallbackData: "review:category"}})
	return &InlineKeyboardMarkup{InlineKeyboard: append(keyboard, []InlineKeyboardButton{{Text: "Abaikan", CallbackData: "review:ignore"}})}
}

func duplicateChoicesMarkup(candidates []string, amounts []string) *InlineKeyboardMarkup {
	return duplicateChoicesMarkupPage(candidates, amounts, 0)
}

func duplicateChoicesMarkupPage(candidates []string, amounts []string, page int) *InlineKeyboardMarkup {
	page = clampChooserPage(page, len(candidates), chooserPageSize)
	start := page * chooserPageSize
	end := start + chooserPageSize
	if end > len(candidates) {
		end = len(candidates)
	}
	keyboard := make([][]InlineKeyboardButton, 0, end-start+2)
	for index := start; index < end; index++ {
		label := "Gabungkan"
		if index < len(amounts) && amounts[index] != "" {
			label = "Gabung Rp" + FormatIDR(amounts[index])
		}
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: clean(label, 40), CallbackData: fmt.Sprintf("review:dup:merge:%d", index)}})
	}
	if pager := pagerRow(page, len(candidates), chooserPageSize, "review:duppage:"); len(pager) > 0 {
		keyboard = append(keyboard, pager)
	}
	keyboard = append(keyboard, []InlineKeyboardButton{{Text: "Catat sebagai baru", CallbackData: "review:dup:new"}, {Text: "Abaikan", CallbackData: "review:ignore"}})
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func (p *Processor) processDuplicatePageCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	page, ok := parsePageCallback(data, "review:duppage:")
	if !ok {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	var reviewID, requestStatus, transactionStatus string
	var rawCandidates []byte
	err = tx.QueryRow(ctx, `SELECT r.id::text,r.status,t.status,COALESCE(rc.context_json->'duplicate_candidates','[]'::jsonb)
		FROM review_request r JOIN transaction t ON t.id=r.transaction_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN review_conversation rc ON rc.review_request_id=r.id
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		  AND r.review_type='POSSIBLE_DUPLICATE'
		FOR UPDATE OF r`, householdID, update.Message.Chat.ID, update.Message.MessageID).
		Scan(&reviewID, &requestStatus, &transactionStatus, &rawCandidates)
	if errors.Is(err, pgx.ErrNoRows) || requestStatus != "OPEN" || transactionStatus != "NEEDS_REVIEW" {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	var candidateIDs []string
	if json.Unmarshal(rawCandidates, &candidateIDs) != nil || len(candidateIDs) == 0 {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	rows, err := tx.Query(ctx, `SELECT id::text,amount::text FROM transaction WHERE household_id=$1 AND id=ANY($2::uuid[])`, householdID, candidateIDs)
	if err != nil {
		return err
	}
	amountByID := make(map[string]string, len(candidateIDs))
	for rows.Next() {
		var id, amount string
		if err := rows.Scan(&id, &amount); err != nil {
			rows.Close()
			return err
		}
		amountByID[id] = amount
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	amounts := make([]string, len(candidateIDs))
	for index, id := range candidateIDs {
		amounts[index] = amountByID[id]
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	text := reviewFollowUpText(ctx, tx, reviewID, "🟡 Transaksi ini mungkin duplikat", "Pilih catatan yang ingin digabung, atau pilih Catat sebagai baru di bawah.")
	if err := enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, text, duplicateChoicesMarkupPage(candidateIDs, amounts, page)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func renderDuplicateChoices(ctx context.Context, tx pgx.Tx, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate) error {
	candidateIDs, markup, err := duplicateCandidateChoices(ctx, tx, householdID, transactionID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(candidateIDs)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,metadata_json) VALUES ($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, sourceEventID, reviewID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_DETAIL',context_json=context_json||jsonb_build_object('duplicate_candidates',$2::jsonb),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID, string(encoded)); err != nil {
		return err
	}
	text := reviewFollowUpText(ctx, tx, reviewID, "🟡 Transaksi ini mungkin duplikat", "Pilih catatan yang ingin digabung, atau pilih Catat sebagai baru di bawah.")
	if update.CallbackQuery != nil {
		return enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, text, markup)
	}
	return enqueueReviewMessageWithMarkup(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, text, markup)
}

func duplicateCandidateChoices(ctx context.Context, tx pgx.Tx, householdID, transactionID string) ([]string, *InlineKeyboardMarkup, error) {
	var sourceType, sourceCurrency, sourceAmount string
	var sourceAt time.Time
	if err := tx.QueryRow(ctx, `SELECT type::text,amount::text,currency,transaction_at FROM transaction WHERE id=$1 AND household_id=$2`, transactionID, householdID).Scan(&sourceType, &sourceAmount, &sourceCurrency, &sourceAt); err != nil {
		return nil, nil, err
	}
	candidates, err := duplicateCandidateRows(ctx, tx, householdID, transactionID, sourceType, sourceCurrency, sourceAmount, sourceAt)
	if err != nil {
		return nil, nil, err
	}
	var candidateIDs, candidateAmounts []string
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.ID)
		candidateAmounts = append(candidateAmounts, candidate.Amount)
	}
	return candidateIDs, duplicateChoicesMarkup(candidateIDs, candidateAmounts), nil
}

func projectDuplicateChoices(ctx context.Context, tx pgx.Tx, reviewID string) (*InlineKeyboardMarkup, error) {
	var householdID, transactionID string
	if err := tx.QueryRow(ctx, `SELECT household_id::text,COALESCE(transaction_id::text,'') FROM review_request WHERE id=$1`, reviewID).Scan(&householdID, &transactionID); err != nil {
		return nil, err
	}
	if transactionID == "" {
		return duplicateIntentMarkup(), nil
	}
	candidateIDs, markup, err := duplicateCandidateChoices(ctx, tx, householdID, transactionID)
	if err != nil {
		return nil, err
	}
	if len(candidateIDs) == 0 {
		return duplicateIntentMarkup(), nil
	}
	encoded, err := json.Marshal(candidateIDs)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('duplicate_candidates',$2::jsonb),updated_at=now() WHERE review_request_id=$1`, reviewID, string(encoded)); err != nil {
		return nil, err
	}
	return markup, nil
}

func (p *Processor) transactionConfirmableWithoutCategory(ctx context.Context, tx pgx.Tx, transactionID string) bool {
	var kind string
	var categoryID *string
	if err := tx.QueryRow(ctx, `SELECT type,category_id::text FROM transaction WHERE id=$1 FOR UPDATE`, transactionID).Scan(&kind, &categoryID); err != nil {
		return false
	}
	return kind != "EXPENSE" || categoryID != nil
}

func reviewNeedsCategory(ctx context.Context, tx pgx.Tx, reviewID string) bool {
	var decision []byte
	if err := tx.QueryRow(ctx, `SELECT ri.decision FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, reviewID).Scan(&decision); err != nil {
		return true
	}
	var stored struct {
		MissingFacts []string `json:"missingFacts"`
	}
	if json.Unmarshal(decision, &stored) != nil {
		return true
	}
	return contains(stored.MissingFacts, "category")
}

type duplicateCandidate struct {
	ID, Amount, Merchant string
	At                   time.Time
}

func duplicateCandidateRows(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, householdID, transactionID, sourceType, sourceCurrency, sourceAmount string, sourceAt time.Time) ([]duplicateCandidate, error) {
	rows, err := q.Query(ctx, `WITH prior AS (SELECT jsonb_array_elements_text(ri.decision->'knownFacts'->'prior_match_transaction_ids') AS id FROM review_item ri WHERE ri.household_id=$1 AND ri.transaction_id=$2 AND jsonb_typeof(ri.decision->'knownFacts'->'prior_match_transaction_ids')='array')
		SELECT t.id::text,t.amount::text,COALESCE(t.counterparty_name,t.description,'Transaksi'),t.transaction_at FROM transaction t WHERE t.household_id=$1 AND t.id<>$2 AND (t.status='CONFIRMED' OR (t.status='NEEDS_REVIEW' AND t.id::text IN (SELECT id FROM prior))) AND ((t.type=$3 AND t.currency=$4 AND t.amount=$5::numeric AND t.transaction_at BETWEEN $6::timestamptz-interval '72 hours' AND $6::timestamptz+interval '72 hours') OR (t.type=$3 AND t.currency=$4 AND t.id::text IN (SELECT id FROM prior))) ORDER BY (t.id::text IN (SELECT id FROM prior)) DESC,abs(extract(epoch FROM (t.transaction_at-$6::timestamptz)))`, householdID, transactionID, sourceType, sourceCurrency, sourceAmount, sourceAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []duplicateCandidate
	for rows.Next() {
		var candidate duplicateCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Amount, &candidate.Merchant, &candidate.At); err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, rows.Err()
}
