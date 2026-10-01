package telegram

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func (p *Processor) applyTransferReviewCallback(ctx context.Context, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate, callback string) error {
	var classification, message string
	classification = transferReviewCallbackAction(callback)
	switch classification {
	case "OWN_ACCOUNT":
		message = "Transfer diklasifikasikan sebagai perpindahan rekening dan tidak dihitung sebagai pengeluaran."
	case "HOUSEHOLD_ACCOUNT":
		message = "Transfer dicatat sebagai perpindahan antar anggota household."
	case "INVESTMENT_ACCOUNT":
		message = "Transfer diklasifikasikan sebagai kontribusi investasi."
	case "IGNORE":
		return p.resolveTransferReview(ctx, sourceEventID, householdID, reviewID, transactionID, update, "UNCLASSIFIED", "VOIDED", "IGNORE", "Transfer disimpan sebagai bukti non-pengeluaran.", "")
	case "EXPENSE":
		return p.offerCategoryChooser(ctx, sourceEventID, householdID, reviewID, transactionID, "TRANSFER_CLASSIFICATION", update)
	case "ASSET_PURCHASE":
		return p.promptAssetWealthAccount(ctx, sourceEventID, householdID, update)
	default:
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Aksi ini sudah selesai atau tidak lagi tersedia.")
	}
	return p.resolveTransferReview(ctx, sourceEventID, householdID, reviewID, transactionID, update, "TRANSFER", "CONFIRMED", classification, message, "")
}

func transferReviewCallbackAction(callback string) string {
	switch callback {
	case "review:own":
		return "OWN_ACCOUNT"
	case "review:household":
		return "HOUSEHOLD_ACCOUNT"
	case "review:investment":
		return "INVESTMENT_ACCOUNT"
	case "review:ignore":
		return "IGNORE"
	case "review:expense":
		return "EXPENSE"
	case "review:asset":
		return "ASSET_PURCHASE"
	default:
		return ""
	}
}

// promptAssetWealthAccount re-asks only the one server-owned missing fact. The
// callback is an exact asset-purchase intent; the wealth-account name itself is
// free text the typed review lane resolves inside the household, so the prompt
// states a suggested shape instead of parsing the answer here.
func (p *Processor) promptAssetWealthAccount(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reviewID, transactionID string
	err = tx.QueryRow(ctx, `SELECT r.id,r.transaction_id FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() FOR UPDATE`, householdID, update.Message.Chat.ID, update.Message.MessageID).Scan(&reviewID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_CONFIRMATION',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if err = enqueueReply(ctx, tx, update, "Sebutkan akun kekayaan tujuan, misalnya: emas."); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) resolveTransferReview(ctx context.Context, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate, newType, newStatus, classification, message, categoryID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return p.resolveTransferReviewTx(ctx, tx, sourceEventID, householdID, reviewID, transactionID, update, newType, newStatus, classification, message, categoryID, "")
}

func (p *Processor) resolveTransferReviewTx(ctx context.Context, tx pgx.Tx, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate, newType, newStatus, classification, message, categoryID, selectedWealthID string) error {
	var err error
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	wealthHint := strings.TrimSpace(update.Message.Text)
	if classification == "ASSET_PURCHASE" {
		if wealthHint == "" {
			return p.continueReview(ctx, sourceEventID, reviewID, transactionID, update, "Sebutkan akun kekayaan tujuan, misalnya: beli emas.")
		}
		id, resolveErr := resolveUniqueWealthHint(ctx, tx, householdID, wealthHint)
		if resolveErr != nil {
			return p.continueReview(ctx, sourceEventID, reviewID, transactionID, update, "Akun kekayaan belum dapat dikenali secara unik. Sebutkan nama yang lebih spesifik.")
		}
		wealthHint = id
	}
	if classification == "INVESTMENT_ACCOUNT" {
		wealthHint = selectedWealthID
	}
	// ADR-046: the transfer mutation, candidate resolution, proposal/source-event
	// refresh, and review completion are one shared operation.
	transfer, err := reviewdomain.ClassifyTransferReview(ctx, tx, reviewdomain.TransferCommand{
		HouseholdID: householdID, ActorUserID: userID, TransactionID: transactionID,
		ReviewItemID: pendingReviewItemID(ctx, tx, reviewID), RequestID: reviewID,
		Action: "TELEGRAM_TRANSFER_CLASSIFIED", Classification: classification,
		CategoryID: categoryID, WealthAccountID: wealthHint,
	})
	if err != nil {
		if errors.Is(err, reviewdomain.ErrInvestmentAccountAmbiguous) {
			_ = tx.Rollback(ctx)
			return p.offerInvestmentChooser(ctx, sourceEventID, householdID, reviewID, transactionID, update)
		}
		if errors.Is(err, reviewdomain.ErrWealthAccountIncompatible) {
			_ = tx.Rollback(ctx)
			return p.continueReview(ctx, sourceEventID, reviewID, transactionID, update, "Akun kekayaan tujuan bukan aset yang kompatibel.")
		}
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification',$4::text)) ON CONFLICT DO NOTHING`, transactionID, sourceEventID, reviewID, classification); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CLASSIFY_TRANSFER','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'classification',$5::text,'type',$6::text,'purpose',$7::text,'related_wealth_account_id',NULLIF($8,'')::text,'status',$9::text))`, householdID, userID, transactionID, reviewID, classification, transfer.Type, transfer.Purpose, transfer.WealthAccountID, transfer.Status); err != nil {
		return err
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// offerInvestmentChooser keeps ambiguous Known Account mapping in Telegram.
// The stored review and callback are bound to real account IDs; the shared
// classifier performs the final household/compatibility check under the lock.
func (p *Processor) offerInvestmentChooser(ctx context.Context, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var open bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN telegram_identity ti ON ti.telegram_user_id=$5 AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active WHERE r.id=$1 AND r.household_id=$2 AND r.transaction_id=$3 AND ri.status IN ('OPEN','PENDING_SEND') AND r.status='OPEN' AND r.expires_at>now() AND rr.telegram_chat_id=$4)`, reviewID, householdID, transactionID, update.Message.Chat.ID, update.Message.From.ID).Scan(&open); err != nil {
		return err
	}
	if !open {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	// ponytail: first ten investment accounts only; paginate if a household grows beyond ten.
	rows, err := tx.Query(ctx, `SELECT id::text,name FROM wealth_account WHERE household_id=$1 AND active AND side='ASSET' AND usage_role='INVESTMENT' ORDER BY name,id LIMIT 10`, householdID)
	if err != nil {
		return err
	}
	var buttons [][]InlineKeyboardButton
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		buttons = append(buttons, []InlineKeyboardButton{{Text: clean(name, 28), CallbackData: "review:invest:" + id}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(buttons) == 0 {
		_ = tx.Rollback(ctx)
		return p.continueReview(ctx, sourceEventID, reviewID, transactionID, update, "Belum ada akun kekayaan investasi aktif untuk dipilih. Tinjauan tetap terbuka sampai rekening tersedia.")
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	markup := &InlineKeyboardMarkup{InlineKeyboard: buttons}
	if update.CallbackQuery != nil {
		err = enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, "Pilih akun kekayaan investasi tujuan:", markup)
	} else {
		err = enqueueReviewMessageWithMarkup(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, "Pilih akun kekayaan investasi tujuan:", markup)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) processInvestmentCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reviewID, transactionID string
	err = tx.QueryRow(ctx, `SELECT r.id::text,r.transaction_id::text FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id JOIN transaction t ON t.id=r.transaction_id JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.review_type='TRANSFER_CLASSIFICATION' AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND') AND t.status='NEEDS_REVIEW' FOR UPDATE OF t,ri`, householdID, update.Message.Chat.ID, update.Message.MessageID, update.Message.From.ID).Scan(&reviewID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	selectedID := strings.TrimPrefix(data, "review:invest:")
	var valid string
	err = tx.QueryRow(ctx, `SELECT id::text FROM wealth_account WHERE id::text=$1 AND household_id=$2 AND transfer_wealth_compatible('INVESTMENT_CONTRIBUTION',id,$2::uuid)`, selectedID, householdID).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	return p.resolveTransferReviewTx(ctx, tx, sourceEventID, householdID, reviewID, transactionID, update, "TRANSFER", "CONFIRMED", "INVESTMENT_ACCOUNT", "Transfer diklasifikasikan sebagai kontribusi investasi.", "", valid)
}
