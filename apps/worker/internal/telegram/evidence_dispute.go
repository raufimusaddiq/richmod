package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func evidenceDisputeTool() gateway.ToolDefinition {
	return gateway.ToolDefinition{Name: "dispute_evidence_link", Description: "Stage a review ONLY when the user explicitly says the exactly replied-to MERGED screenshot is a different/new transaction, not the already linked event. No IDs or amounts accepted. Go validates the current single-row link. Nothing is confirmed until a human presses the review confirmation button.", Parameters: objectSchema(map[string]any{}, []string{})}
}

func evidenceDisputeMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Konfirmasi transaksi berbeda", CallbackData: "review:dispute:confirm"}, {Text: "Batalkan koreksi", CallbackData: "review:dispute:reject"}}}}
}

func (p *Processor) agentDisputeEvidenceLink(ctx context.Context, state *agentState, call gateway.ToolCall) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect, Status: "EVIDENCE_DISPUTE_INVALID"}
	document, found, err := p.resolveReplyEvidence(ctx, state.HouseholdID, state.Update)
	if err != nil {
		return result, false, err
	}
	if !found || state.Update.Message.From.ID <= 0 {
		return result, false, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return result, false, err
	}
	defer tx.Rollback(ctx)
	var user string
	err = tx.QueryRow(ctx, `SELECT ti.user_id::text FROM telegram_identity ti JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
		JOIN source_event se ON se.id=$3::uuid AND se.household_id=ti.household_id AND se.source_type='TELEGRAM_TEXT'
		WHERE ti.telegram_user_id=$1 AND ti.household_id=$2 AND ti.active FOR SHARE OF ti,hm,se`, state.Update.Message.From.ID, state.HouseholdID, state.SourceEventID).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "EVIDENCE_DISPUTE_UNAUTHORIZED"
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	staged, err := reviewdomain.StageEvidenceDispute(ctx, tx, reviewdomain.EvidenceDisputeCommand{HouseholdID: state.HouseholdID, ActorUserID: user, DocumentID: string(document), SourceEventID: state.SourceEventID})
	if errors.Is(err, reviewdomain.ErrEvidenceDisputeInvalid) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if staged.Created {
		decision, _ := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", staged.TransactionID)
		decision.Provenance["evidence_link_dispute"] = true
		decision.Provenance["disputed_document_id"] = string(document)
		decision.WhyNotAuto = "the household disputes a previous screenshot link; explicit human button confirmation is required"
		if err = EnqueueReviewRequest(ctx, tx, staged.TransactionID, "POSSIBLE_DUPLICATE", state.Update.Message.Chat.ID, state.Update.Message.MessageID, "Koreksi tautan screenshot belum dicatat. Konfirmasi hanya jika ini benar-benar transaksi berbeda. Catatan lama tetap utuh; screenshot akan dialihkan ke catatan baru setelah konfirmasi.", decision); err != nil {
			return result, false, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-evidence-dispute',parser_version='1' WHERE id=$1::uuid`, state.SourceEventID); err != nil {
		return result, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, false, err
	}
	result.Status = "EVIDENCE_DISPUTE_STAGED"
	if !staged.Created {
		result.Status = "EVIDENCE_DISPUTE_ALREADY_STAGED"
		result.Facts = map[string]any{"status": staged.Status}
	}
	return result, false, nil
}

func (p *Processor) processEvidenceDisputeCallback(ctx context.Context, source, household string, update telegramUpdate, data string) (bool, error) {
	if data != "review:dispute:confirm" && data != "review:dispute:reject" {
		return false, nil
	}
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID <= 0 {
		return true, p.finishWithoutTransaction(ctx, source, "IGNORED", update, "Koreksi tidak diubah. Identitas Telegram tidak dapat diverifikasi.")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var request, item, transaction, user string
	err = tx.QueryRow(ctx, `SELECT r.id::text,ri.id::text,ri.transaction_id::text,ti.user_id::text
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN telegram_identity ti ON ti.household_id=r.household_id AND ti.telegram_user_id=$4 AND ti.active
		JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND')
		AND ri.decision->'decisionProvenance'->>'evidence_link_dispute'='true' FOR UPDATE OF r,ri`, household, update.Message.Chat.ID, update.Message.MessageID, update.CallbackQuery.From.ID).Scan(&request, &item, &transaction, &user)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, finishStaleReviewCallback(ctx, tx, source, update)
	}
	if err != nil {
		return true, err
	}
	confirm := data == "review:dispute:confirm"
	err = reviewdomain.ResolveEvidenceDispute(ctx, tx, reviewdomain.EvidenceDisputeCommand{HouseholdID: household, ActorUserID: user, TransactionID: transaction, ReviewItemID: item, RequestID: request}, confirm)
	if errors.Is(err, reviewdomain.ErrEvidenceDisputeInvalid) || errors.Is(err, reviewdomain.ErrAlreadyResolved) {
		if err = tx.Rollback(ctx); err != nil {
			return true, err
		}
		return true, p.finishWithoutTransaction(ctx, source, "IGNORED", update, "Koreksi belum dicatat: tautan screenshot berubah atau tidak lagi tunggal. Tinjau screenshot dan catatan di Kotak Tinjauan; jangan mencatat salinan otomatis.")
	}
	if err != nil {
		return true, fmt.Errorf("resolve screenshot dispute: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-evidence-dispute',parser_version='1' WHERE id=$1::uuid`, source); err != nil {
		return true, err
	}
	message := "Koreksi dibatalkan. Screenshot tetap terkait dengan catatan semula; tidak ada transaksi baru yang dikonfirmasi."
	if confirm {
		message = "Transaksi berbeda sudah dikonfirmasi. Screenshot sekarang terkait dengan catatan baru. Catatan lama dan bukti asli tetap tersimpan."
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
