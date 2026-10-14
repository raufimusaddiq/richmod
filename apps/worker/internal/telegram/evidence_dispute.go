package telegram

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/merchantmemory"
)

func evidenceDisputeTool() gateway.ToolDefinition {
	return gateway.ToolDefinition{Name: "dispute_evidence_link", Description: "Stage a review ONLY when the user explicitly says the exactly replied-to MERGED screenshot is a different/new transaction, not the already linked event. No IDs or amounts accepted. Go validates the current single-row link. Nothing is confirmed until a human presses the review confirmation button.", Parameters: objectSchema(map[string]any{}, []string{})}
}

func evidenceDisputeMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Konfirmasi transaksi berbeda", CallbackData: "review:dispute:confirm"}, {Text: "Batalkan koreksi", CallbackData: "review:dispute:reject"}}}}
}

// disputeCategory is the category Go resolves for the disputed screenshot's own
// extraction, with the same policy the normal screenshot path applies: a
// validated, active household category slug; household merchant memory
// overrides it only when both agree, and a disagreement leaves it unresolved
// (never confirm through a conflict) so the household is asked.
type disputeCategory struct{ id string }

func (p *Processor) disputeCategory(ctx context.Context, tx pgx.Tx, household, document string) (disputeCategory, error) {
	var slug, merchant, candidate *string
	var confidence *float64
	err := tx.QueryRow(ctx, `SELECT x.slug,x.merchant,x.confidence,p.category_candidate_id::text
		FROM document d JOIN transaction_proposal p ON p.source_event_id=d.source_event_id AND p.household_id=d.household_id
		LEFT JOIN LATERAL (SELECT de.output_json->'transactions'->0->>'category_slug' AS slug,de.output_json->'transactions'->0->>'merchant' AS merchant,(de.output_json->'transactions'->0->>'category_confidence')::float8 AS confidence
			FROM document_extraction de WHERE de.document_id=d.id AND de.stage='TRANSACTION_SCREENSHOT' AND de.validated AND jsonb_array_length(de.output_json->'transactions')=1
			ORDER BY de.created_at DESC LIMIT 1) x ON true
		WHERE d.id=$1::uuid AND d.household_id=$2 LIMIT 1`, document, household).Scan(&slug, &merchant, &confidence, &candidate)
	if errors.Is(err, pgx.ErrNoRows) {
		return disputeCategory{}, nil
	}
	if err != nil {
		return disputeCategory{}, err
	}
	active := func(column, value string) (string, error) {
		var id string
		err := tx.QueryRow(ctx, `SELECT id::text FROM category WHERE household_id=$1 AND `+column+`=$2 AND active`, household, value).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return id, err
	}
	var bySlug string
	if slug != nil && *slug != "" {
		if bySlug, err = active("slug", *slug); err != nil {
			return disputeCategory{}, err
		}
	}
	var memory *merchantmemory.Match
	if merchant != nil {
		if memory, err = merchantmemory.Lookup(ctx, tx, household, *merchant); err != nil {
			return disputeCategory{}, err
		}
	}
	switch {
	case memory != nil && bySlug != "" && memory.CategoryID != bySlug:
		return disputeCategory{}, nil
	case memory != nil:
		return disputeCategory{id: memory.CategoryID}, nil
	case bySlug != "":
		return disputeCategory{id: bySlug}, nil
	case candidate != nil && *candidate != "":
		id, err := active("id::text", *candidate)
		return disputeCategory{id: id}, err
	}
	return disputeCategory{}, nil
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
	category, err := p.disputeCategory(ctx, tx, state.HouseholdID, string(document))
	if err != nil {
		return result, false, err
	}
	staged, err := reviewdomain.StageEvidenceDispute(ctx, tx, reviewdomain.EvidenceDisputeCommand{HouseholdID: state.HouseholdID, ActorUserID: user, DocumentID: string(document), SourceEventID: state.SourceEventID, CategoryID: category.id})
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

const disputeStaleMessage = "Koreksi belum dicatat: tautan screenshot berubah atau tidak lagi tunggal. Tinjau screenshot dan catatan di Kotak Tinjauan; jangan mencatat salinan otomatis."

var disputeCategoryID = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// disputeRequest locks the open dispute card the callback's message is bound to,
// for an active household member. Nothing else resolves a dispute.
func disputeRequest(ctx context.Context, tx pgx.Tx, household string, update telegramUpdate) (request, item, transaction, user string, err error) {
	err = tx.QueryRow(ctx, `SELECT r.id::text,ri.id::text,ri.transaction_id::text,ti.user_id::text
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN telegram_identity ti ON ti.household_id=r.household_id AND ti.telegram_user_id=$4 AND ti.active
		JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND')
		AND ri.decision->'decisionProvenance'->>'evidence_link_dispute'='true' FOR UPDATE OF r,ri`, household, update.Message.Chat.ID, update.Message.MessageID, update.CallbackQuery.From.ID).Scan(&request, &item, &transaction, &user)
	return
}

func (p *Processor) processEvidenceDisputeCallback(ctx context.Context, source, household string, update telegramUpdate, data string) (bool, error) {
	if data != "review:dispute:confirm" && data != "review:dispute:reject" {
		return false, nil
	}
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID <= 0 {
		return true, p.finishWithoutTransaction(ctx, source, "IGNORED", update, "Koreksi tidak diubah. Identitas Telegram tidak dapat diverifikasi.")
	}
	return true, p.resolveEvidenceDispute(ctx, source, household, update, data == "review:dispute:confirm", "")
}

// resolveEvidenceDispute confirms (optionally with the household's chosen category)
// or rejects the dispute the callback's card is bound to, atomically.
func (p *Processor) resolveEvidenceDispute(ctx context.Context, source, household string, update telegramUpdate, confirm bool, categoryID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	request, item, transaction, user, err := disputeRequest(ctx, tx, household, update)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, source, update)
	}
	if err != nil {
		return err
	}
	err = reviewdomain.ResolveEvidenceDispute(ctx, tx, reviewdomain.EvidenceDisputeCommand{HouseholdID: household, ActorUserID: user, TransactionID: transaction, ReviewItemID: item, RequestID: request, CategoryID: categoryID}, confirm)
	switch {
	case errors.Is(err, reviewdomain.ErrMissingCategory):
		// No policy-resolved category: ask through the existing chooser, record nothing.
		if err = tx.Rollback(ctx); err != nil {
			return err
		}
		return p.offerDisputeCategoryChooser(ctx, source, request, update, 0)
	case errors.Is(err, reviewdomain.ErrInvalidCandidate):
		if err = tx.Rollback(ctx); err != nil {
			return err
		}
		return p.finishWithoutTransaction(ctx, source, "IGNORED", update, "Kategori itu tidak tersedia untuk keluarga ini, jadi belum ada yang dicatat. Pilih kategori dari daftar.")
	case errors.Is(err, reviewdomain.ErrEvidenceDisputeInvalid), errors.Is(err, reviewdomain.ErrAlreadyResolved):
		if err = tx.Rollback(ctx); err != nil {
			return err
		}
		return p.finishWithoutTransaction(ctx, source, "IGNORED", update, disputeStaleMessage)
	case err != nil:
		return fmt.Errorf("resolve screenshot dispute: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-evidence-dispute',parser_version='1' WHERE id=$1::uuid`, source); err != nil {
		return err
	}
	message := "Koreksi dibatalkan. Screenshot tetap terkait dengan catatan semula; tidak ada transaksi baru yang dikonfirmasi."
	if confirm {
		message = "Transaksi berbeda sudah dikonfirmasi. Screenshot sekarang terkait dengan catatan baru. Catatan lama dan bukti asli tetap tersimpan."
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// disputeCategoryMarkup is the existing category chooser without its generic
// asset-purchase/detail/ignore row, which would bypass the dispute contract.
func disputeCategoryMarkup(markup *InlineKeyboardMarkup) *InlineKeyboardMarkup {
	if markup == nil {
		return nil
	}
	rows := markup.InlineKeyboard
	if len(rows) > 0 {
		rows = rows[:len(rows)-1]
	}
	rows = append(append([][]InlineKeyboardButton{}, rows...), []InlineKeyboardButton{{Text: "Batalkan koreksi", CallbackData: "review:dispute:reject"}})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (p *Processor) offerDisputeCategoryChooser(ctx context.Context, source, request string, update telegramUpdate, page int) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	markup := disputeCategoryMarkup(reviewActionMarkupPage(ctx, tx, request, "POSSIBLE_DUPLICATE", page))
	if markup == nil {
		if err = enqueueReply(ctx, tx, update, "Belum ada kategori aktif yang bisa dipilih."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-evidence-dispute',parser_version='1' WHERE id=$1::uuid`, source); err != nil {
		return err
	}
	if err = enqueueReviewUpdateWithMarkup(ctx, tx, request, update, fmt.Sprintf("Kategori transaksi ini belum pasti. Pilih kategori (halaman %d) untuk mencatatnya sebagai transaksi berbeda:", page+1), markup); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// processDisputeCategoryCallback handles the chooser taps of a dispute card:
// paging, or the household's explicit category that completes the confirmation.
func (p *Processor) processDisputeCategoryCallback(ctx context.Context, source, household string, update telegramUpdate, data string) error {
	if strings.HasPrefix(data, "review:catpage:") {
		page, err := strconv.Atoi(strings.TrimPrefix(data, "review:catpage:"))
		if err != nil || page < 0 || page > 1000 {
			return p.finishWithoutTransaction(ctx, source, "IGNORED", update, staleActionMessage)
		}
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		request, _, _, _, err := disputeRequest(ctx, tx, household, update)
		if err != nil {
			_ = tx.Rollback(ctx)
			return p.finishWithoutTransaction(ctx, source, "IGNORED", update, staleActionMessage)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return p.offerDisputeCategoryChooser(ctx, source, request, update, page)
	}
	categoryID := strings.TrimPrefix(data, "review:cat:")
	if !disputeCategoryID.MatchString(categoryID) {
		return p.finishWithoutTransaction(ctx, source, "IGNORED", update, staleActionMessage)
	}
	return p.resolveEvidenceDispute(ctx, source, household, update, true, categoryID)
}
