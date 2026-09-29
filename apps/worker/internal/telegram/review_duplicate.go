package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func duplicateIntentMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Catat sebagai baru", CallbackData: "review:dup:new"}, {Text: "Abaikan", CallbackData: "review:ignore"}}}}
}

func reviewDetailMarkup() *InlineKeyboardMarkup {
	keyboard := [][]InlineKeyboardButton{{{Text: "Merchant", CallbackData: "review:merchant"}, {Text: "Deskripsi", CallbackData: "review:description"}}}
	keyboard = append(keyboard, []InlineKeyboardButton{{Text: "Kategori", CallbackData: "review:category"}})
	return &InlineKeyboardMarkup{InlineKeyboard: append(keyboard, []InlineKeyboardButton{{Text: "Abaikan", CallbackData: "review:ignore"}})}
}

// duplicateChoicesMarkup renders one button per stored duplicate candidate. The
// candidate transaction IDs never travel through Telegram. The callback carries
// the list position, and the server-owned ordered candidate list stored with the
// projection on the first render resolves it. Candidate IDs are revalidated by
// the shared merge operation, so a stale or retargeted button cannot select a
// different canonical transaction.
func duplicateChoicesMarkup(candidates []string, amounts []string) *InlineKeyboardMarkup {
	keyboard := make([][]InlineKeyboardButton, 0, len(candidates)+1)
	for index := range candidates {
		if index >= 9 {
			break
		}
		label := "Gabungkan"
		if amounts[index] != "" {
			label = "Gabung Rp" + FormatIDR(amounts[index])
		}
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: clean(label, 40), CallbackData: fmt.Sprintf("review:dup:merge:%d", index)}})
	}
	keyboard = append(keyboard, []InlineKeyboardButton{{Text: "Catat sebagai baru", CallbackData: "review:dup:new"}, {Text: "Abaikan", CallbackData: "review:ignore"}})
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

// offerDuplicateChoices sends the duplicate decision with one button per stored
// candidate, so the ordinary blocker is completable inside Telegram.
func (p *Processor) offerDuplicateChoices(ctx context.Context, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := renderDuplicateChoices(ctx, tx, sourceEventID, householdID, reviewID, transactionID, update); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// renderDuplicateChoices writes the candidate buttons and persists the ordered
// candidate list the callbacks resolve against.
func renderDuplicateChoices(ctx context.Context, tx pgx.Tx, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate) error {
	var sourceType, sourceCurrency string
	var sourceAmount string
	var sourceAt time.Time
	if err := tx.QueryRow(ctx, `SELECT type::text,amount::text,currency,transaction_at FROM transaction WHERE id=$1 AND household_id=$2`, transactionID, householdID).Scan(&sourceType, &sourceAmount, &sourceCurrency, &sourceAt); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.amount::text FROM transaction t WHERE t.household_id=$1 AND t.id<>$2 AND t.status='CONFIRMED' AND t.type=$3 AND t.currency=$4 AND t.amount=$5::numeric AND t.transaction_at BETWEEN $6::timestamptz-interval '72 hours' AND $6::timestamptz+interval '72 hours' ORDER BY abs(extract(epoch FROM (t.transaction_at-$6::timestamptz))) LIMIT 9`, householdID, transactionID, sourceType, sourceCurrency, sourceAmount, sourceAt)
	if err != nil {
		return err
	}
	var candidateIDs, candidateAmounts []string
	for rows.Next() {
		var candidateID, candidateAmount string
		if err := rows.Scan(&candidateID, &candidateAmount); err != nil {
			rows.Close()
			return err
		}
		candidateIDs = append(candidateIDs, candidateID)
		candidateAmounts = append(candidateAmounts, candidateAmount)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// ponytail: one page of up to 9 candidates; page the list when a review can
	// legitimately carry more (the API caps financial-email candidates at 10).
	markup := duplicateChoicesMarkup(candidateIDs, candidateAmounts)
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
	if err = enqueueReviewMessageWithMarkup(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, "Transaksi ini mungkin duplikat. Pilih gabung dengan catatan yang sudah ada atau catat sebagai transaksi baru.", markup); err != nil {
		return err
	}
	return nil
}

// transactionConfirmableWithoutCategory reports whether the shared confirm rule
// accepts this transaction with no category. An uncategorized EXPENSE needs a
// category, so a date-only save must continue to the chooser instead of trying to
// confirm and failing the expense-category invariant.
func (p *Processor) transactionConfirmableWithoutCategory(ctx context.Context, tx pgx.Tx, transactionID string) bool {
	var kind string
	var categoryID *string
	if err := tx.QueryRow(ctx, `SELECT type,category_id::text FROM transaction WHERE id=$1 FOR UPDATE`, transactionID).Scan(&kind, &categoryID); err != nil {
		return false
	}
	return kind != "EXPENSE" || categoryID != nil
}

// reviewNeedsCategory reports whether the stored decision still lists category as
// an unresolved fact, so a field save only continues to the chooser when the
// decision actually asked for a category.
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
