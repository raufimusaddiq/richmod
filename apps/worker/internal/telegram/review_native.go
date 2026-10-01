package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) extractReview(ctx context.Context, sourceEventID, text string, categories []categoryChoice) (reviewExtraction, error) {
	if p.gateway == nil {
		return reviewExtraction{}, fmt.Errorf("review classifier unavailable")
	}
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
	}
	call, metadata, err := p.gateway.NativeToolCall(ctx, sourceEventID, reviewPrompt,
		map[string]any{"reply": "<untrusted_user_message>" + text + "</untrusted_user_message>", "allowed_category_slugs": slugs},
		[]gateway.ToolDefinition{{Name: "resolve_review", Description: "Resolve one already-bound finance review using bounded values.", Parameters: reviewSchema(slugs, len(categories) > 0)}}, gateway.NativeToolOptions{Required: true})
	if err != nil {
		return reviewExtraction{}, err
	}
	result, err := gateway.DecodeToolArguments[reviewExtraction](call, "resolve_review")
	if err != nil {
		return reviewExtraction{}, err
	}
	_ = metadata
	if result.Confidence < 0 || result.Confidence > 1 {
		return reviewExtraction{}, fmt.Errorf("review confidence outside range")
	}
	result.Description = clean(result.Description, 500)
	result.Note = clean(result.Note, 1000)
	return result, nil
}

func (p *Processor) resolveNativeReview(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, args map[string]any) error {
	action, _ := args["action"].(string)
	if handled, err := p.resolveNativeSpecialReview(ctx, sourceEventID, householdID, update, action, args); handled {
		return err
	}
	// A provider-email evidence residual is observation-bound and its only bounded
	// action is IGNORE, so resolve it directly instead of falling through to the
	// transaction-bound lanes below (SAVR-06).
	if action == "IGNORE" {
		if handled, err := p.ignoreFinancialEmailFacts(ctx, sourceEventID, householdID, update); handled {
			return err
		}
	}
	if update.Message.ReplyToMessage != nil {
		var requestID, itemID, caseID string
		err := p.pool.QueryRow(ctx, `SELECT r.id,ri.id,crc.id FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND r.review_type='CYCLE_RESIDUAL_ALLOCATION' AND r.status='OPEN' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3`, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID).Scan(&requestID, &itemID, &caseID)
		if err == nil {
			return p.resolveNativeResidualReview(ctx, sourceEventID, householdID, update, requestID, itemID, caseID, action, args)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	query := `SELECT r.id,r.transaction_id,t.type,r.review_type,c.state,COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0),COALESCE(ri.decision,'{}'::jsonb) FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id LEFT JOIN review_item ri ON ri.id=r.review_item_id WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2 ORDER BY r.created_at DESC LIMIT 2`
	params := []any{householdID, update.Message.Chat.ID}
	if update.Message.ReplyToMessage != nil {
		query = `SELECT r.id,r.transaction_id,t.type,r.review_type,c.state,COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0),COALESCE(ri.decision,'{}'::jsonb) FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id LEFT JOIN review_item ri ON ri.id=r.review_item_id WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 LIMIT 2`
		params = append(params, update.Message.ReplyToMessage.MessageID)
	}
	rows, err := p.pool.Query(ctx, query, params...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type candidate struct {
		id, tx, typ, reviewType, state, merchantID string
		messageID                                  int64
		decision                                   []byte
	}
	var choices []candidate
	for rows.Next() {
		var v candidate
		if err := rows.Scan(&v.id, &v.tx, &v.typ, &v.reviewType, &v.state, &v.merchantID, &v.messageID, &v.decision); err != nil {
			return err
		}
		choices = append(choices, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(choices) != 1 {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Ada beberapa review aktif. Pilih pesan review yang ingin diselesaikan atau balas pesannya.")
	}
	categorySlug, _ := args["category_slug"].(string)
	description, _ := args["description"].(string)
	merchant, _ := args["merchant"].(string)
	payDate, _ := args["pay_date"].(string)
	amountIDR, _ := args["amount_idr"].(string)
	transactionAt, _ := args["transaction_at"].(string)
	c := choices[0]
	// IR-02: the reply lane must satisfy the stored residual contract before it
	// confirms. A generic reply that leaves a required fact unsupplied asks for
	// that exact fact instead of confirming a transaction with a placeholder.
	if blocked := residualConfirmationBlockers(c.decision, validReviewDate(payDate), categorySlug != "", strings.TrimSpace(merchant) != ""); len(blocked) > 0 {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, reviewNeedsFactsMessage(blocked))
	}
	if action == "SET_PAY_DATE" && !validReviewDate(payDate) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Tanggal pembayaran wajib diisi dengan format YYYY-MM-DD.")
	}
	if action == "COMPLETE_BANK_FACTS" && (strings.TrimSpace(amountIDR) == "" || !validReviewTimestamp(transactionAt)) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Nominal dan waktu transaksi lengkap diperlukan untuk melanjutkan review bank.")
	}
	if action == "PRIMARY_SALARY" || action == "ORDINARY_INCOME" {
		choice := salaryChoicePrimary
		if action == "ORDINARY_INCOME" {
			choice = salaryChoiceOrdinary
		}
		_, err := p.executePendingSalaryChoice(ctx, householdID, update, sourceEventID, choice)
		return err
	}
	if action == "IGNORE" {
		return p.rejectBoundReview(ctx, sourceEventID, householdID, c.id, c.tx, update)
	}
	if field, detail, required := requiredNativeReviewDetail(c.reviewType, c.state, c.merchantID, merchant, description); required {
		if detail == "" {
			message := "Nama merchant wajib diisi sebelum review dapat diselesaikan."
			if field == "description" {
				message = "Keterangan transaksi wajib diisi sebelum review dapat diselesaikan."
			}
			return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, message)
		}
		if c.messageID == 0 {
			return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Pesan tinjauan belum terikat. Buka Kotak Tinjauan untuk melanjutkan.")
		}
		update.Message.Text = detail
		update.Message.ReplyToMessage = &struct {
			MessageID int64 `json:"message_id"`
		}{MessageID: c.messageID}
		return p.saveBoundReviewField(ctx, sourceEventID, householdID, c.id, c.tx, update, field)
	}
	if action == "OWN_ACCOUNT_TRANSFER" {
		return p.resolveTransferReview(ctx, sourceEventID, householdID, c.id, c.tx, update, "TRANSFER", "CONFIRMED", "OWN_ACCOUNT", "Transfer diklasifikasikan sebagai perpindahan rekening dan tidak dihitung sebagai pengeluaran.", "")
	}
	if action == "HOUSEHOLD_TRANSFER" {
		return p.resolveTransferReview(ctx, sourceEventID, householdID, c.id, c.tx, update, "TRANSFER", "CONFIRMED", "HOUSEHOLD_ACCOUNT", "Transfer dicatat sebagai perpindahan antar anggota household.", "")
	}
	if action == "INVESTMENT_TRANSFER" {
		return p.resolveTransferReview(ctx, sourceEventID, householdID, c.id, c.tx, update, "TRANSFER", "CONFIRMED", "INVESTMENT_ACCOUNT", "Transfer diklasifikasikan sebagai kontribusi investasi.", "")
	}
	if action == "ASSET_PURCHASE" {
		wealthHint, _ := args["wealth_account_hint"].(string)
		if strings.TrimSpace(wealthHint) == "" {
			return p.continueReview(ctx, sourceEventID, c.id, c.tx, update, "Sebutkan akun kekayaan tujuan, misalnya: emas.")
		}
		update.Message.Text = wealthHint
		return p.resolveTransferReview(ctx, sourceEventID, householdID, c.id, c.tx, update, "TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Pembelian aset dicatat sebagai transfer.", "")
	}
	categoryID := ""
	if categorySlug != "" {
		if err := p.pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, householdID, categorySlug).Scan(&categoryID); err != nil {
			return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Kategori belum valid untuk keluarga ini.")
		}
	}
	if c.typ == "UNCLASSIFIED" && action == "EXPENSE" {
		if categoryID == "" {
			return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Pilih kategori pengeluaran terlebih dahulu.")
		}
		return p.resolveTransferReview(ctx, sourceEventID, householdID, c.id, c.tx, update, "EXPENSE", "CONFIRMED", "EXPENSE", "Transfer dicatat sebagai pengeluaran.", categoryID)
	}
	return p.resolveReview(ctx, sourceEventID, householdID, c.id, c.tx, categoryID, update, reviewExtraction{Description: clean(description, 500), Note: clean(merchant, 1000), PayDate: payDate, Confidence: 1})
}

func (p *Processor) resolveNativeSpecialReview(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, action string, args map[string]any) (bool, error) {
	var caseID, itemID string
	var candidates []string
	var replyID int64
	if update.Message.ReplyToMessage != nil {
		replyID = update.Message.ReplyToMessage.MessageID
	} else if update.CallbackQuery != nil {
		replyID = update.Message.MessageID
	}
	err := p.pool.QueryRow(ctx, `SELECT trc.id::text,ri.id::text,trc.candidate_transaction_ids FROM review_request rr JOIN review_request_recipient recipient ON recipient.review_request_id=rr.id JOIN review_item ri ON ri.id=rr.review_item_id JOIN transfer_reconciliation_case trc ON trc.household_id=ri.household_id AND ((ri.financial_email_observation_id IS NOT NULL AND trc.financial_email_observation_id=ri.financial_email_observation_id) OR (ri.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id)) WHERE rr.household_id=$1 AND rr.status='OPEN' AND ri.status IN ('OPEN','PENDING_SEND') AND trc.status='OPEN' AND recipient.telegram_chat_id=$2 AND recipient.telegram_message_id=$3 AND $3<>0`, householdID, update.Message.Chat.ID, replyID).Scan(&caseID, &itemID, &candidates)
	if err == nil {
		if action == "MERGE_EXISTING" {
			ref, _ := args["candidate_ref"].(string)
			var index int
			if _, scanErr := fmt.Sscanf(ref, "candidate_%d", &index); scanErr != nil || index < 1 || index > len(candidates) {
				return true, p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Pilih kandidat transfer yang tersedia.")
			}
			return true, p.resolveNativeTransferCase(ctx, sourceEventID, householdID, update, caseID, itemID, candidates[index-1], "MERGE_EXISTING")
		}
		if action == "CONFIRM_NEW_TRANSFER" {
			return true, p.resolveNativeTransferCase(ctx, sourceEventID, householdID, update, caseID, itemID, "", "CONFIRM_NEW_TRANSFER")
		}
		if action == "IGNORE" {
			return true, p.resolveNativeTransferCase(ctx, sourceEventID, householdID, update, caseID, itemID, "", "IGNORE")
		}
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Pilih tindakan rekonsiliasi transfer yang valid.")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return true, err
	}
	var observationID, institution, hint, originalSource string
	// Bind the wealth observation to the card the user actually replied to, not
	// just the newest PENDING observation in the household. Without the message
	// join an IGNORE on one card (such as a provider-email facts card) could
	// resolve an unrelated pending wealth observation (SAVR-06, Hermes).
	err = p.pool.QueryRow(ctx, `SELECT wo.id::text,wo.institution,wo.account_hint,d.source_event_id FROM wealth_observation wo JOIN review_item ri ON ri.wealth_observation_id=wo.id JOIN review_request r ON r.review_item_id=ri.id JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN document d ON d.id=wo.document_id WHERE wo.household_id=$1 AND wo.status='PENDING' AND ri.status IN ('OPEN','PENDING_SEND') AND r.status='OPEN' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND $3<>0 ORDER BY wo.created_at DESC LIMIT 1`, householdID, update.Message.Chat.ID, replyID).Scan(&observationID, &institution, &hint, &originalSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if action == "SET_WEALTH_ACCOUNT" {
		wealthHint, _ := args["wealth_account_hint"].(string)
		tx, txErr := p.pool.Begin(ctx)
		if txErr != nil {
			return true, txErr
		}
		defer tx.Rollback(ctx)
		id, resolveErr := resolveUniqueWealthHint(ctx, tx, householdID, wealthHint)
		if resolveErr != nil {
			return true, p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Akun kekayaan harus cocok tepat satu.")
		}
		// ADR-046: the observation mutation and review-learned alias are shared.
		if txErr = reviewdomain.ResolveWealthObservation(ctx, tx, reviewdomain.WealthObservationCommand{HouseholdID: householdID, ObservationID: observationID, WealthAccountID: id}); txErr != nil {
			return true, p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Akun kekayaan tidak lagi tersedia. Pilih ulang rekeningnya.")
		}
		if _, txErr = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, sourceEventID); txErr != nil {
			return true, txErr
		}
		if txErr = enqueueReply(ctx, tx, update, "Akun kekayaan tersimpan. Siapkan snapshot lengkap untuk menerapkan nilai dokumen."); txErr != nil {
			return true, txErr
		}
		return true, tx.Commit(ctx)
	}
	if action == "RECORD_ASSET_PURCHASE" {
		sourceHint, _ := args["source_account_hint"].(string)
		wealthHint, _ := args["wealth_account_hint"].(string)
		amount, _ := args["amount_idr"].(string)
		at, _ := args["transaction_at"].(string)
		if strings.TrimSpace(sourceHint) == "" || strings.TrimSpace(at) == "" || !validReviewTimestamp(at) {
			return true, p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Sebutkan rekening sumber dan waktu transaksi lengkap, misalnya: Bank Jago, 2026-08-26T08:00:00+07:00.")
		}
		if strings.TrimSpace(wealthHint) == "" {
			wealthHint = strings.TrimSpace(institution + " " + hint)
		}
		if strings.TrimSpace(amount) == "" {
			if err := p.pool.QueryRow(ctx, `SELECT observed_value_idr::text FROM wealth_observation WHERE id=$1 AND status='PENDING'`, observationID).Scan(&amount); err != nil {
				return true, err
			}
		}
		parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(at))
		local := parsed.In(jakartaLocation())
		if err := p.recordTransfer(ctx, sourceEventID, householdID, update, map[string]any{
			"amount_idr": amount, "source_account_hint": sourceHint, "destination_wealth_account_hint": wealthHint,
			"reclassification_purpose": "ASSET_PURCHASE", "date_reference": "EXPLICIT", "explicit_date": local.Format("2006-01-02"),
			"local_time": local.Format("15:04"), "description": "Pembelian investasi dari bukti Telegram",
		}); err != nil {
			return true, err
		}
		var transactionID string
		if err := p.pool.QueryRow(ctx, `SELECT transaction_id::text FROM transaction_evidence WHERE source_event_id=$1 ORDER BY created_at DESC LIMIT 1`, sourceEventID).Scan(&transactionID); errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		} else if err != nil {
			return true, err
		}
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return true, err
		}
		defer tx.Rollback(ctx)
		var userID string
		if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_IMAGE',1,jsonb_build_object('reclassified_from','WEALTH_OBSERVATION','observation_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, originalSource, observationID); err != nil {
			return true, err
		}
		// ADR-046: the observation dismissal and evidence reclassification are shared.
		if err = reviewdomain.DismissWealthObservation(ctx, tx, reviewdomain.WealthObservationCommand{HouseholdID: householdID, ObservationID: observationID}); err != nil && !errors.Is(err, reviewdomain.ErrWealthObservationNotFound) {
			return true, err
		}
		if err = reviewdomain.ReclassifyWealthEvidence(ctx, tx, observationID); err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='RECLASSIFIED_ASSET_PURCHASE',updated_at=now() WHERE wealth_observation_id=$1 AND status IN ('OPEN','PENDING_SEND')`, observationID, userID); err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, originalSource); err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RECLASSIFY_WEALTH_OBSERVATION','wealth_observation',$3,jsonb_build_object('transaction_id',$4::uuid,'purpose','ASSET_PURCHASE'))`, householdID, userID, observationID, transactionID); err != nil {
			return true, err
		}
		if err = tx.Commit(ctx); err != nil {
			return true, err
		}
		return true, nil
	}
	if action == "IGNORE" {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Observasi kekayaan diabaikan.")
	}
	return true, p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Pilih tindakan observasi kekayaan yang valid.")
}

func (p *Processor) resolveNativeTransferCase(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, caseID, itemID, target, action string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	if _, err := reviewdomain.ReconcileTransfer(ctx, tx, reviewdomain.TransferReconciliationCommand{
		HouseholdID: householdID, ActorUserID: userID, ReviewItemID: itemID,
		CaseID: caseID, Action: action, CandidateID: target, ActorType: "TELEGRAM",
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1 AND household_id=$2`, sourceEventID, householdID); err != nil {
		return err
	}
	message := "Rekonsiliasi transfer tersimpan tanpa duplikasi."
	if action == "IGNORE" {
		message = "Bukti transfer diabaikan."
	}
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type residualAllocation struct {
	WealthAccountID string `json:"wealth_account_id"`
	AmountIDR       string `json:"amount_idr"`
	Note            string `json:"note"`
}

func (p *Processor) resolveNativeResidualReview(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, requestID, itemID, caseID, action string, args map[string]any) error {
	if action == "TRANSACTION_MISSING" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, "Kirim transaksi yang belum tercatat sebagai pesan baru di sini (jangan balas kartu tinjauan). Setelah transaksi tersimpan, sisa siklus gaji dihitung ulang; tinjauan tetap terbuka jika masih perlu tindakan.")
	}
	if action != "ALLOCATE_RETAINED_BALANCE" && action != "LEAVE_UNALLOCATED" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Pilih alokasikan saldo tersisa, biarkan belum dialokasikan, atau catat transaksi baru lewat Telegram.")
	}
	var input struct {
		Allocations []residualAllocation `json:"allocations"`
	}
	encoded, err := json.Marshal(args)
	if err != nil || json.Unmarshal(encoded, &input) != nil {
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, "Data alokasi tidak valid.")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	var lockedRequest, lockedItem string
	if err = tx.QueryRow(ctx, `SELECT r.id,ri.id FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1 AND ri.id=$2 AND r.household_id=$3 AND r.status='OPEN' AND ri.status IN ('PENDING_SEND','OPEN') FOR UPDATE`, requestID, itemID, householdID).Scan(&lockedRequest, &lockedItem); err != nil {
		return err
	}
	// ADR-046: cycle basis refresh, allocation validation, and completion live in
	// the shared operation. Telegram keeps its own binding, replies, and audit.
	allocations := make([]reviewdomain.CycleAllocation, 0, len(input.Allocations))
	for _, allocation := range input.Allocations {
		allocations = append(allocations, reviewdomain.CycleAllocation{WealthAccountID: allocation.WealthAccountID, AmountIDR: allocation.AmountIDR, Note: allocation.Note})
	}
	outcome, err := reviewdomain.ApplyCycleResidual(ctx, tx, reviewdomain.CycleResidualCommand{
		HouseholdID: householdID, CaseID: caseID, ReviewItemID: itemID, RequestID: requestID,
		Action: action, Allocations: allocations, ActorUserID: userID,
	})
	if err != nil {
		// Validation failures keep the review open with the same guidance the
		// inline path returned, so a user can correct and retry.
		return p.finishWithoutTransaction(ctx, sourceEventID, "NEEDS_REVIEW", update, residualReviewGuidance(err))
	}
	switch outcome.Outcome {
	case reviewdomain.CycleStaleNotApplicable:
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CYCLE_RESIDUAL_STALE_NOT_APPLICABLE','cycle_residual_case',$3,jsonb_build_object('residualIdr',$4))`, householdID, userID, caseID, outcome.Residual); err != nil {
			return err
		}
		if err = enqueueReply(ctx, tx, update, "Sisa siklus gaji tidak lagi positif. Rekonsiliasi ini ditutup tanpa alokasi."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	case reviewdomain.CycleStaleRefreshed:
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CYCLE_RESIDUAL_STALE','cycle_residual_case',$3,jsonb_build_object('residualIdr',$4))`, householdID, userID, caseID, outcome.Residual); err != nil {
			return err
		}
		if err = enqueueReply(ctx, tx, update, "Basis sisa cycle berubah. Tinjau nominal terbaru, lalu selesaikan lagi."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,$3,'cycle_residual_case',$4,jsonb_build_object('residualIdr',$5))`, householdID, userID, "CYCLE_RESIDUAL_"+action, caseID, outcome.Residual); err != nil {
		return err
	}
	if err = enqueueReply(ctx, tx, update, "Rekonsiliasi sisa siklus gaji tersimpan."); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// residualReviewGuidance maps a shared cycle-residual validation error to the
// Indonesian guidance the inline Telegram path returned, so a user can correct
// the same way regardless of which lane reached the operation.
func requiredNativeReviewDetail(reviewType, state, merchantID, merchant, description string) (field, value string, required bool) {
	if state == "AWAITING_MERCHANT" || (reviewType == "UNKNOWN_MERCHANT" && merchantID == "" && strings.TrimSpace(merchant) != "") {
		return "merchant", clean(strings.TrimSpace(merchant), 500), true
	}
	if state == "AWAITING_DATE" {
		return "transaction_at", clean(strings.TrimSpace(description), 500), true
	}
	if reviewType == "UNKNOWN_PURPOSE" || state == "AWAITING_DETAIL" {
		return "description", clean(strings.TrimSpace(description), 500), true
	}
	return "", "", false
}

func validReviewDate(value string) bool {
	_, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return err == nil
}

func validReviewTimestamp(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return err == nil && !parsed.IsZero() && parsed.Format(time.RFC3339) != ""
}

// missingFactsAreCategoryOnly reports a stored ReviewDecision whose only unresolved
// fact is the category. That is the whole interaction, so the Telegram chooser can
// complete it instead of deferring to the Review Inbox. A legacy review without a
// stored contract keeps its previous behavior and does not qualify.
func missingFactsAreCategoryOnly(raw *string) bool {
	if raw == nil || strings.TrimSpace(*raw) == "null" {
		return false
	}
	var facts []string
	if json.Unmarshal([]byte(*raw), &facts) != nil {
		return false
	}
	return len(facts) == 1 && facts[0] == "category"
}

// offerCategoryChooser renders the household category chooser for a review whose only
// unresolved fact is the category, so a merchant-less bank transaction is completable
// inside Telegram. It reuses the same paged markup the initial send uses.
func (p *Processor) offerCategoryChooser(ctx context.Context, sourceEventID, householdID, reviewID, transactionID, reviewType string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	markup := reviewActionMarkupPage(ctx, tx, reviewID, reviewType, 0)
	if markup == nil {
		if err = enqueueReply(ctx, tx, update, "Tidak ada kategori aktif untuk dipilih."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if update.CallbackQuery != nil {
		// The chooser is already on screen, so edit it in place.
		err = enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, "Pilih kategori pengeluaran:", markup)
	} else {
		err = enqueueReviewMessageWithMarkup(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, "Pilih kategori pengeluaran:", markup)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) continueReview(ctx context.Context, sourceEventID, reviewID, transactionID string, update telegramUpdate, message string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,metadata_json) VALUES ($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, sourceEventID, reviewID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_CATEGORY',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if err := enqueueReviewMessage(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) resolveReview(ctx context.Context, sourceEventID, householdID, reviewID, transactionID, categoryID string, update telegramUpdate, value reviewExtraction) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	if err := p.resolveReviewTx(ctx, tx, sourceEventID, householdID, reviewID, transactionID, categoryID, update, value, userID, true); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) resolveReviewTx(ctx context.Context, tx pgx.Tx, sourceEventID, householdID, reviewID, transactionID, categoryID string, update telegramUpdate, value reviewExtraction, userID string, offerMerchantLearning bool) error {
	var err error
	var merchantID *string
	// IR-02: a legacy card or a client that omits a field must not confirm while
	// the stored ReviewDecision still reports a canonical-required residual fact.
	// The date check uses the parsed pay date only; a fallback timestamp never
	// satisfies it.
	var storedDecision []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(ri.decision,'{}'::jsonb) FROM review_request rr JOIN review_item ri ON ri.id=rr.review_item_id WHERE rr.id=$1 AND ri.household_id=$2 AND ri.status IN ('PENDING_SEND','OPEN') FOR UPDATE OF ri`, reviewID, householdID).Scan(&storedDecision); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	payDate, payDateErr := parseSuppliedReviewDate(value.PayDate)
	if payDateErr != nil {
		return payDateErr
	}
	if blocked := residualConfirmationBlockers(storedDecision, payDate != nil, categoryID != "", false); len(blocked) > 0 {
		return enqueueReply(ctx, tx, update, reviewNeedsFactsMessage(blocked))
	}
	var transactionAt *time.Time
	if payDate != nil {
		parsed, err := time.ParseInLocation("2006-01-02", *payDate, jakartaLocation())
		if err != nil {
			return err
		}
		transactionAt = &parsed
	}
	// ADR-046: the transaction mutation and candidate revalidation live in the
	// shared canonical resolver. Telegram keeps only its own delivery/evidence and
	// payslip side effects, and defers terminal completion while it asks whether to
	// remember the merchant.
	var requestID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM review_request WHERE id=$1 AND household_id=$2`, reviewID, householdID).Scan(&requestID); err != nil {
		return err
	}
	confirmResult, err := reviewdomain.ConfirmTransactionReview(ctx, tx, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: userID, TransactionID: transactionID,
		ReviewItemID: pendingReviewItemID(ctx, tx, reviewID), RequestID: requestID,
		Action: "TELEGRAM_CONFIRMED", CategorySupplied: categoryID != "", CategoryID: categoryID,
		Description: value.Description, Note: value.Note, TransactionAt: transactionAt,
		ResolveReview: false,
	})
	if err != nil {
		return err
	}
	merchantID = &confirmResult.MerchantID
	if confirmResult.MerchantID == "" {
		merchantID = nil
	}
	// A confirmed review also completes the document workflow (ADR-046: shared op).
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, tx, transactionID); err != nil {
		return err
	}
	if value.PayDate != "" {
		// A user-supplied pay date completes only a payslip-backed income review.
		// Evidence and period come from the shared payslip reader; no expected
		// payday is ever inferred.
		facts, ok, err := reviewdomain.LoadPayslipFacts(ctx, tx, transactionID)
		if err != nil {
			return err
		}
		if ok && reviewPayrollPeriodPattern.MatchString(facts.Period) {
			if _, err := reviewdomain.RecordSalaryEvent(ctx, tx, reviewdomain.SalaryCommand{
				HouseholdID: householdID, Employer: facts.Employer, Period: facts.Period,
				PayDate: value.PayDate, NetPay: facts.NetPay,
				Transaction: transactionID, SourceEvent: sourceEventID,
			}); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, transactionID); err != nil {
		return err
	}
	askRemember := offerMerchantLearning && merchantID != nil && categoryID != ""
	if askRemember {
		// Complete the review item now so a merchant-learning question the user
		// never answers cannot leave the review open (UIR-08). The pending question
		// is tracked by conversation state, not by review_request.status.
		if err := resolveCanonicalReviewItem(ctx, tx, reviewID, userID, "TELEGRAM_CONFIRMED"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_MERCHANT_DECISION',context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID, categoryID); err != nil {
			return err
		}
	} else {
		if err := resolveCanonicalReviewItem(ctx, tx, reviewID, userID, "TELEGRAM_CONFIRMED"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID, categoryID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES ($2,$1,'TELEGRAM_REVIEW_REPLY',$3,jsonb_build_object('review_request_id',$4::uuid,'classification','CONFIRM')) ON CONFLICT DO NOTHING`, sourceEventID, transactionID, value.Confidence, reviewID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'TELEGRAM',$2,'RESOLVE_REVIEW','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'category_id',NULLIF($5,'')::uuid))`, householdID, userID, transactionID, reviewID, categoryID); err != nil {
		return err
	}
	if askRemember {
		markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ingat merchant", CallbackData: "review:remember"}, {Text: "Sekali ini", CallbackData: "review:once"}}}}
		if err := enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, "Tercatat. Ingat kategori ini untuk merchant tersebut?", markup); err != nil {
			return err
		}
	} else if err := enqueueReply(ctx, tx, update, "Tercatat dan Kotak Tinjauan sudah diperbarui."); err != nil {
		return err
	}
	return nil
}

func (p *Processor) applyMerchantLearningChoice(ctx context.Context, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate, remember bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	if remember {
		var merchantID, categoryID string
		if err = tx.QueryRow(ctx, `SELECT merchant_id,category_id FROM transaction WHERE id=$1 AND household_id=$2 AND status='CONFIRMED' AND merchant_id IS NOT NULL AND category_id IS NOT NULL`, transactionID, householdID).Scan(&merchantID, &categoryID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 AND household_id=$1 ON CONFLICT(household_id,lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g'))) DO UPDATE SET raw_name=excluded.raw_name,default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, householdID, merchantID, categoryID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REMEMBER_MERCHANT','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'merchant_id',$5::uuid,'category_id',$6::uuid))`, householdID, userID, transactionID, reviewID, merchantID, categoryID); err != nil {
			return err
		}
	}
	if err = resolveCanonicalReviewItem(ctx, tx, reviewID, userID, "TELEGRAM_MERCHANT_DECISION"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($3,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$1::uuid,'remember_merchant',$4::boolean)) ON CONFLICT DO NOTHING`, reviewID, sourceEventID, transactionID, remember); err != nil {
		return err
	}
	message := "Kategori merchant tidak disimpan sebagai aturan."
	if remember {
		message = "Kategori merchant disimpan dan dapat dinonaktifkan di Settings."
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func reviewSchema(slugs []string, categoriesRequired ...bool) map[string]any {
	sort.Strings(slugs)
	properties := map[string]any{
		"description": map[string]any{"type": "string"},
		"note":        map[string]any{"type": "string"},
		"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"ambiguous":   map[string]any{"type": "boolean"},
		"pay_date":    map[string]any{"type": "string"},
	}
	required := []string{"description", "note", "confidence", "ambiguous", "pay_date"}
	if len(categoriesRequired) == 0 || categoriesRequired[0] {
		properties["category_slug"] = map[string]any{"type": "string", "enum": slugs}
		required = append(required, "category_slug")
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": properties,
		"required":   required,
	}
}

func ReviewQuestion(amount, merchant string) string {
	if strings.TrimSpace(merchant) == "" {
		merchant = "transaksi ini"
	}
	return "🟡 Perlu ditinjau\n\n" +
		"Nominal: Rp" + FormatIDR(amount) + "\n" +
		"Merchant: " + merchant + "\n\n" +
		"Balas pesan ini dengan tujuan pengeluaran, atau pilih kategori di bawah."
}

func FormatIDR(value string) string {
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", strings.TrimPrefix(value, "-")
	}
	if len(value) <= 3 {
		return sign + value
	}
	first := len(value) % 3
	if first == 0 {
		first = 3
	}
	parts := []string{value[:first]}
	for index := first; index < len(value); index += 3 {
		parts = append(parts, value[index:index+3])
	}
	return sign + strings.Join(parts, ".")
}

// resolveNativeMerchantLearning handles explicit native confirmation without
// parsing free-form text. It reuses the audited transactional executor.
func (p *Processor) resolveNativeMerchantLearning(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, args map[string]any) error {
	remember, _ := args["remember"].(bool)
	var reviewID, transactionID string
	err := p.pool.QueryRow(ctx, `SELECT r.id,r.transaction_id FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND c.state='AWAITING_MERCHANT_DECISION' AND t.status='CONFIRMED' AND rr.telegram_chat_id=$2 ORDER BY r.created_at DESC LIMIT 1`, householdID, update.Message.Chat.ID).Scan(&reviewID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Tidak ada konfirmasi merchant yang aktif.")
	}
	if err != nil {
		return err
	}
	return p.applyMerchantLearningChoice(ctx, sourceEventID, householdID, reviewID, transactionID, update, remember)
}
