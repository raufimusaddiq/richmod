package telegram

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func (p *Processor) ignoreBankReview(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var itemID, userID string
	err = tx.QueryRow(ctx, `SELECT ri.id::text,ti.user_id::text
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=r.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND')
		AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.transaction_id IS NULL FOR UPDATE OF ri`, householdID, update.Message.Chat.ID, update.Message.MessageID, update.CallbackQuery.From.ID).Scan(&itemID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if err = reviewdomain.IgnoreBankReview(ctx, tx, reviewdomain.BankFactCommand{HouseholdID: householdID, UserID: userID, ReviewItemID: itemID}, "TELEGRAM"); err != nil {
		if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) {
			return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1 AND household_id=$2`, sourceEventID, householdID); err != nil {
		return true, err
	}
	if err = enqueueReply(ctx, tx, update, "Bukti email bank diabaikan."); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func (p *Processor) completeBankFactsReply(ctx context.Context, sourceEventID, householdID, reviewID, userID, bankSourceID string, update telegramUpdate) error {
	amountIDR, transactionAt := parseBankFactsReply(update.Message.Text)
	if reviewdomain.ValidateBankFactValues(amountIDR, transactionAt) != nil {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Balas nominal dan waktu transaksi, contoh: 54000 2026-09-23T13:45:00+07:00.")
	}
	at, parseErr := time.Parse(time.RFC3339, transactionAt)
	if parseErr != nil {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Waktu transaksi wajib format ISO 8601 dengan zona waktu, contoh: 2026-09-23T13:45:00+07:00.")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := enqueueBankFactsCompletion(ctx, tx, reviewdomain.BankFactCommand{HouseholdID: householdID, UserID: userID, ReviewItemID: reviewID, SourceEventID: bankSourceID, AmountIDR: amountIDR, TransactionAt: at.Format(time.RFC3339)}, update.Message.Chat.ID); err != nil {
		if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) || errors.Is(err, reviewdomain.ErrAlreadyResolved) {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		if errors.Is(err, reviewdomain.ErrBankSourceUnlinked) {
			accounts, listErr := reviewdomain.ListBankSourceAccountChoices(ctx, tx, householdID, reviewID, bankSourceID, 10)
			if listErr != nil {
				return listErr
			}
			if len(accounts) == 0 {
				return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Email bank ini belum terhubung ke rekening, dan belum ada rekening aktif. Tambahkan rekening lebih dulu.")
			}
			_ = tx.Rollback(ctx)
			return p.offerBankAccountChooser(ctx, sourceEventID, householdID, reviewID, accounts, amountIDR, transactionAt, update)
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if err := enqueueReply(ctx, tx, update, bankFactsQueuedMessage); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const bankFactsQueuedMessage = "Fakta bank diterima untuk diproses. Transaksi belum dicatat; status review akan diperbarui setelah pemrosesan berhasil."

// enqueueBankFactsCompletion validates household-supplied bank facts against the
// review and its listener, then queues the shared COMPLETE_BANK_REVIEW job. It
// validates fast, so a missing account re-prompts instead of queueing a job that
// can never complete, but leaves resolution and persistence to the job, exactly
// like the Web lane: resolving here would make the job a no-op and drop the
// household's facts. Errors are the reviewdomain sentinels, for the caller to
// word.
func enqueueBankFactsCompletion(ctx context.Context, tx pgx.Tx, command reviewdomain.BankFactCommand, chatID int64) error {
	if err := reviewdomain.ValidateBankSourceAccount(ctx, tx, command); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('COMPLETE_BANK_REVIEW',jsonb_build_object('source_event_id',$1::uuid,'review_id',$2::uuid,'amount_idr',$3::text,'transaction_at',$4::text,'telegram_chat_id',$5::bigint))`, command.SourceEventID, command.ReviewItemID, command.AmountIDR, command.TransactionAt, chatID)
	return err
}

func (p *Processor) offerBankAccountChooser(ctx context.Context, sourceEventID, householdID, reviewID string, accounts []reviewdomain.BankAccountChoice, amount, at string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var requestID string
	if err := tx.QueryRow(ctx, `SELECT r.id::text FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.review_item_id=$1 AND r.household_id=$2 AND rr.telegram_chat_id=$3 AND r.status='OPEN' AND r.expires_at>now()`, reviewID, householdID, update.Message.Chat.ID).Scan(&requestID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=jsonb_set(context_json,'{bank_pending}',jsonb_build_object('amount_idr',$2::text,'transaction_at',$3::text),true),updated_at=now() WHERE review_request_id=$1`, requestID, amount, at); err != nil {
		return err
	}
	// ponytail: first 10 accounts only; paginate when households exceed ten.
	buttons := make([][]InlineKeyboardButton, 0, len(accounts))
	for _, account := range accounts {
		buttons = append(buttons, []InlineKeyboardButton{{Text: clean(account.Name, 28), CallbackData: "review:bank:" + account.ID}})
	}
	if err := enqueueReviewMessageWithMarkup(ctx, tx, requestID, update.Message.Chat.ID, update.Message.MessageID, "Pilih rekening untuk email bank ini:", &InlineKeyboardMarkup{InlineKeyboard: buttons}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) processBankAccountCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reviewID, bankSourceID, userID, amount, at string
	err = tx.QueryRow(ctx, `SELECT ri.id::text,ri.source_event_id::text,ti.user_id::text,COALESCE(c.context_json#>>'{bank_pending,amount_idr}',''),COALESCE(c.context_json#>>'{bank_pending,transaction_at}','') FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_conversation c ON c.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id JOIN telegram_identity ti ON ti.telegram_user_id=$2 AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='UNKNOWN_BANK_TEMPLATE' FOR UPDATE OF ri`, householdID, update.Message.Chat.ID, update.Message.MessageID).Scan(&reviewID, &bankSourceID, &userID, &amount, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	if reviewdomain.ValidateBankFactValues(amount, at) != nil {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	choices, err := reviewdomain.ListBankSourceAccountChoices(ctx, tx, householdID, reviewID, bankSourceID, 10)
	if err != nil {
		return err
	}
	accountID := strings.TrimPrefix(data, "review:bank:")
	valid := false
	for _, choice := range choices {
		valid = valid || choice.ID == accountID
	}
	if !valid {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err := reviewdomain.BindBankSourceAccount(ctx, tx, householdID, reviewID, bankSourceID, accountID); err != nil {
		if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) || errors.Is(err, reviewdomain.ErrBankAccountInvalid) {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	update.Message.Text = amount + " " + at
	return p.completeBankFactsReply(ctx, sourceEventID, householdID, reviewID, userID, bankSourceID, update)
}

// parseBankFactsReply extracts "<amount> <rfc3339 timestamp>" from a reply, in
// either order, so the user can answer naturally. The amount must be whole IDR
// digits (an optional Rp/IDR prefix is allowed); a value carrying `.` or `,` is
// rejected rather than reshaped, so "54,5" or "12.500,50" can never be silently
// read as a different canonical amount.
func parseBankFactsReply(text string) (string, string) {
	fields := strings.Fields(text)
	amount, at := "", ""
	for _, field := range fields {
		if _, err := time.Parse(time.RFC3339, field); err == nil {
			at = field
			continue
		}
		candidate := strings.TrimSpace(field)
		for _, prefix := range []string{"Rp", "rp", "IDR", "idr"} {
			candidate = strings.TrimPrefix(candidate, prefix)
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.ContainsAny(candidate, ",.") {
			continue
		}
		if reviewdomain.ValidBankAmountIDR(candidate) {
			amount = candidate
		}
	}
	return amount, at
}
