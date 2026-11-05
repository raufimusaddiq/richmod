package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// pendingConfirmationWindow is how long a staged correction or batch waits for
// the household's confirmation. Migration 00092 sets the same column default.
const pendingConfirmationWindow = "30 minutes"

// The Telegram batch lane follows the single-entry rules (ADR-033 amendment,
// 2026-11): an item without a category is staged and later goes to review
// instead of rejecting the batch, each item gets the single entry's duplicate
// checks (shown before confirmation; a document the user just sent is never
// recorded twice), every confirmed item refreshes open cycle residuals, and the
// merchant is resolved to the household's merchant identity.

type agentPendingBatchItem struct {
	Type          string    `json:"Type"`
	Amount        string    `json:"Amount"`
	Merchant      string    `json:"Merchant"`
	CategorySlug  string    `json:"CategorySlug"`
	Description   string    `json:"Description"`
	Note          string    `json:"Note"`
	TransactionAt time.Time `json:"TransactionAt"`
	// SimilarExisting describes an existing transaction the item may repeat
	// (same amount and merchant in the last 7 days). Shown before confirmation.
	SimilarExisting string `json:"SimilarExisting,omitempty"`
	// AlreadyRecorded marks an item a document the user just sent already
	// recorded; confirming the batch never records it again.
	AlreadyRecorded bool `json:"AlreadyRecorded,omitempty"`
}

type pendingBatchPatch struct {
	Index                                       int
	Amount, Merchant, CategorySlug, Description *string
}

func (p *Processor) agentStageBatch(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	raw, ok := args["items"].([]any)
	if !ok || len(raw) < 1 || len(raw) > 10 {
		return result, true, &agentRecoverable{Status: "INVALID_BATCH", Reason: "items must hold 1-10 transactions",
			NextStep: "call record_transaction_batch with 1-10 items, or record_transaction for one",
			UserCopy: "Daftarnya belum bisa kubaca, jadi belum ada yang dicatat. Kirim maksimal 10 transaksi sekaligus, ya."}
	}
	categories, err := p.categorySlugs(ctx, state.HouseholdID)
	if err != nil {
		return result, true, err
	}
	items := make([]agentPendingBatchItem, 0, len(raw))
	for index, entryRaw := range raw {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return result, true, fmt.Errorf("invalid batch item")
		}
		v, err := nativeValidatedExtraction(entry, state.Now)
		if err != nil {
			return result, true, &agentRecoverable{Status: "INVALID_BATCH_ITEM", Reason: fmt.Sprintf("item %d: %v", index+1, err),
				NextStep: "correct that item and call record_transaction_batch again, or ask the user for the missing detail",
				UserCopy: "Ada item di daftar yang belum lengkap, jadi belum ada yang dicatat. Sebutkan nominal dan tanggalnya, ya."}
		}
		item := agentPendingBatchItem{Type: v.Type, Amount: v.Amount, Merchant: v.Merchant, CategorySlug: v.CategorySlug, Description: v.Description, Note: v.Note, TransactionAt: v.TransactionAt}
		// A slug the household does not have is not guessed and does not reject
		// the batch: the item is staged without a category and goes to review.
		if item.CategorySlug != "" && !contains(categories, item.CategorySlug) {
			item.CategorySlug = ""
		}
		if err := p.markBatchItemDuplicates(ctx, state, &item); err != nil {
			return result, true, err
		}
		items = append(items, item)
	}

	encoded, _ := json.Marshal(items)
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status,expires_at) VALUES($1,$2,$3,$4,$5::jsonb,'PENDING',now()+$6::interval) ON CONFLICT(telegram_user_id,telegram_chat_id) WHERE status='PENDING' DO UPDATE SET source_event_id=excluded.source_event_id,items_json=excluded.items_json,confirmation_token=gen_random_uuid(),expires_at=excluded.expires_at,created_at=now() RETURNING confirmation_token::text`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID, state.SourceEventID, string(encoded), pendingConfirmationWindow).Scan(&result.confirmationToken); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-batch-proposal',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	public, total := batchItemsPublic(items)
	result.Status = "PENDING_CONFIRMATION"
	result.Mutation = map[string]any{"action": "BATCH_STAGED", "items": public, "count": len(public), "total_idr": total}
	if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	return result, false, nil
}

// batchItemsPublic is the model- and reply-facing view of staged items.
func batchItemsPublic(items []agentPendingBatchItem) ([]map[string]any, string) {
	total := big.NewInt(0)
	public := make([]map[string]any, 0, len(items))
	for index, item := range items {
		if n, ok := new(big.Int).SetString(item.Amount, 10); ok {
			total.Add(total, n)
		}
		entry := map[string]any{"item_ref": fmt.Sprintf("batch_%d", index+1), "type": item.Type, "amount_idr": item.Amount, "merchant": item.Merchant, "category_slug": item.CategorySlug, "description": item.Description, "note": item.Note, "transaction_at": item.TransactionAt.In(jakartaLocation()).Format(time.RFC3339)}
		if item.SimilarExisting != "" {
			entry["similar_existing"] = item.SimilarExisting
		}
		if item.AlreadyRecorded {
			entry["already_recorded"] = true
		}
		public = append(public, entry)
	}
	return public, total.String()
}

// markBatchItemDuplicates runs the single entry's duplicate checks on one item.
func (p *Processor) markBatchItemDuplicates(ctx context.Context, state *agentState, item *agentPendingBatchItem) error {
	item.SimilarExisting, item.AlreadyRecorded = "", false
	if p.evidenceAlreadyRecorded(ctx, state.HouseholdID, state.FreshEvidenceDocuments, item.Type, item.Amount, item.Merchant) {
		item.AlreadyRecorded = true
		return nil
	}
	if item.Type != "EXPENSE" {
		return nil
	}
	label, at, found, err := p.similarRecentExpense(ctx, state.HouseholdID, item.Amount, item.Merchant, item.TransactionAt)
	if err != nil || !found {
		return err
	}
	item.SimilarExisting = label + " · Rp" + FormatIDR(item.Amount) + " · " + formatIDDateTime(at)
	return nil
}

// similarRecentExpense is the single entry's "did you mean the one you already
// recorded?" lookup: an expense with the same amount whose merchant or
// description matches, in the last 7 days, at a different time.
func (p *Processor) similarRecentExpense(ctx context.Context, householdID, amount, merchant string, at time.Time) (string, time.Time, bool, error) {
	if strings.TrimSpace(merchant) == "" {
		return "", time.Time{}, false, nil
	}
	var label string
	var existingAt time.Time
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(counterparty_name,description,'Transaksi'),transaction_at FROM transaction WHERE household_id=$1 AND status<>'VOIDED' AND type='EXPENSE' AND amount=$2::numeric AND transaction_at>=now()-interval '7 days' AND (counterparty_name ILIKE '%'||$3::text||'%' OR description ILIKE '%'||$3::text||'%') ORDER BY transaction_at DESC LIMIT 1`, householdID, amount, merchant).Scan(&label, &existingAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && existingAt.Equal(at)) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	return label, existingAt, true, nil
}

func (p *Processor) agentPendingBatchDecision(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	action, _ := args["action"].(string)
	switch action {
	case "CONFIRM":
		return p.agentFinalizePendingBatch(ctx, state, call, true, nil)
	case "CANCEL":
		return p.agentFinalizePendingBatch(ctx, state, call, false, nil)
	case "UPDATE":
		ref, _ := args["item_ref"].(string)
		index, err := strconv.Atoi(strings.TrimPrefix(ref, "batch_"))
		if err != nil || index < 1 {
			return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, true, invalidItemRef(ref)
		}
		confirm, _ := args["confirm_after_update"].(bool)
		return p.agentFinalizePendingBatch(ctx, state, call, confirm, &pendingBatchPatch{
			Index:        index - 1,
			Amount:       agentOptionalString(args["amount_idr"]),
			Merchant:     agentOptionalString(args["merchant"]),
			CategorySlug: agentOptionalString(args["category_slug"]),
			Description:  agentOptionalString(args["description"]),
		})
	default:
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, true, &agentRecoverable{Status: "INVALID_BATCH_ACTION", Reason: "action must be CONFIRM, CANCEL or UPDATE",
			NextStep: "call pending_batch_decision with CONFIRM, CANCEL or UPDATE", UserCopy: "Mau dicatat semua, dibatalkan, atau ada item yang diubah?"}
	}
}

func invalidItemRef(ref string) *agentRecoverable {
	return &agentRecoverable{Status: "INVALID_ITEM_REF", Reason: fmt.Sprintf("%q is not an item of the pending batch", ref),
		NextStep: "use an item_ref listed in turn_context.pending_batch, or ask the user which item they mean",
		UserCopy: "Item yang dimaksud belum ketemu di daftar. Sebutkan nomor atau nama itemnya, ya."}
}

func (p *Processor) agentFinalizePendingBatch(ctx context.Context, state *agentState, call gateway.ToolCall, confirm bool, patch *pendingBatchPatch) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var batchID, raw string
	err = tx.QueryRow(ctx, `SELECT id,items_json::text FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID).Scan(&batchID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "NO_PENDING_BATCH"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	var items []agentPendingBatchItem
	if err = json.Unmarshal([]byte(raw), &items); err != nil {
		return result, true, err
	}
	if patch != nil {
		if patch.Index < 0 || patch.Index >= len(items) {
			return result, true, invalidItemRef(fmt.Sprintf("batch_%d", patch.Index+1))
		}
		item := &items[patch.Index]
		if patch.Amount != nil {
			item.Amount = *patch.Amount
		}
		if patch.Merchant != nil {
			item.Merchant = clean(*patch.Merchant, 160)
		}
		if patch.CategorySlug != nil {
			item.CategorySlug = clean(*patch.CategorySlug, 120)
			if item.CategorySlug != "" {
				var exists bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM category WHERE household_id=$1 AND slug=$2 AND active)`, state.HouseholdID, item.CategorySlug).Scan(&exists); err != nil {
					return result, true, err
				}
				if !exists {
					return result, true, &agentRecoverable{Status: "INVALID_CATEGORY", Reason: fmt.Sprintf("%q is not one of allowed_category_slugs", item.CategorySlug),
						NextStep: "use a slug from allowed_category_slugs, or null to leave the item for review",
						UserCopy: "Kategori itu tidak ada di daftar keluarga ini. Pilih kategori yang tersedia, ya."}
				}
			}
		}
		if patch.Description != nil {
			item.Description = clean(*patch.Description, 500)
		}
		if patch.Amount != nil || patch.Merchant != nil {
			if err := p.markBatchItemDuplicates(ctx, state, item); err != nil {
				return result, true, err
			}
		}
		encoded, _ := json.Marshal(items)
		if err = tx.QueryRow(ctx, `UPDATE telegram_pending_batch SET items_json=$2::jsonb,confirmation_token=gen_random_uuid(),expires_at=now()+$3::interval WHERE id=$1 RETURNING confirmation_token::text`, batchID, string(encoded), pendingConfirmationWindow).Scan(&result.confirmationToken); err != nil {
			return result, true, err
		}
	}
	if !confirm {
		if patch == nil {
			if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET status='CANCELLED',resolved_at=now() WHERE id=$1`, batchID); err != nil {
				return result, true, err
			}
			result.Status = "CANCELLED"
			result.Mutation = map[string]any{"action": "BATCH_CANCELLED", "confirmed": false, "count": len(items)}
		} else {
			public, total := batchItemsPublic(items)
			result.Status = "UPDATED"
			result.Mutation = map[string]any{"action": "BATCH_UPDATED", "confirmed": false, "count": len(items), "items": public, "total_idr": total}
		}
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-batch-resolution',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
			return result, true, err
		}
		if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
			return result, true, err
		}
		if err = tx.Commit(ctx); err != nil {
			return result, true, err
		}
		return result, false, nil
	}

	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	outcome, err := p.confirmPendingBatchTx(ctx, tx, state.HouseholdID, batchID, items, userID, state.Update)
	if err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-batch-confirmation',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	result.Status = "CONFIRMED"
	result.Mutation = map[string]any{"action": "BATCH_RECORDED", "confirmed": true, "count": len(items),
		"recorded": outcome.Confirmed, "needs_review": outcome.NeedsReview, "skipped_already_recorded": outcome.Skipped}
	if outcome.NeedsReview > 0 {
		result.Review = map[string]any{"required": true, "reason": "BATCH_ITEMS_NEED_CATEGORY", "card_in_chat": true}
	}
	if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	return result, false, nil
}

type batchConfirmOutcome struct {
	Confirmed, NeedsReview, Skipped int
}

// confirmPendingBatchTx records a confirmed batch. The household's explicit
// confirmation is the authority for the facts it was shown; structural checks,
// merchant memory and the category rule still apply. An expense without a
// usable category is recorded as NEEDS_REVIEW with a review card in this chat
// (never rejected, never guessed); an item a just-sent document already
// recorded is skipped. Every confirmed item refreshes open cycle residuals.
// Both the agent and the button path use it.
func (p *Processor) confirmPendingBatchTx(ctx context.Context, tx pgx.Tx, householdID, batchID string, items []agentPendingBatchItem, userID string, update telegramUpdate) (batchConfirmOutcome, error) {
	var outcome batchConfirmOutcome
	allowedCategories, err := p.categorySlugs(ctx, householdID)
	if err != nil {
		return outcome, err
	}
	for _, v := range items {
		n, ok := new(big.Int).SetString(v.Amount, 10)
		if !ok || n.Sign() <= 0 || n.String() != v.Amount {
			return outcome, TerminalError(fmt.Errorf("invalid batch amount"))
		}
		if v.Type != "INCOME" && v.Type != "EXPENSE" {
			return outcome, TerminalError(fmt.Errorf("invalid batch type"))
		}
		if v.TransactionAt.IsZero() {
			return outcome, TerminalError(fmt.Errorf("invalid batch time"))
		}
	}
	var sourceEventID string
	if err := tx.QueryRow(ctx, `SELECT source_event_id FROM telegram_pending_batch WHERE id=$1`, batchID).Scan(&sourceEventID); err != nil {
		return outcome, err
	}
	for i, v := range items {
		if v.AlreadyRecorded {
			outcome.Skipped++
			continue
		}
		merchantID, remembered, err := resolveEntryMerchantTx(ctx, tx, householdID, v.Merchant)
		if err != nil {
			return outcome, err
		}
		slug := v.CategorySlug
		if slug != "" && !contains(allowedCategories, slug) {
			slug = ""
		}
		if slug == "" && remembered != nil && v.Type == "EXPENSE" {
			slug = remembered.Slug
		}
		var cat *string
		if slug != "" {
			var cid string
			err = tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, householdID, slug).Scan(&cid)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return outcome, err
			}
			if err == nil {
				cat = &cid
			}
		}
		status := "CONFIRMED"
		if v.Type == "EXPENSE" && cat == nil {
			status = "NEEDS_REVIEW"
		}
		proposalStatus := "ACCEPTED"
		if status == "NEEDS_REVIEW" {
			proposalStatus = "NEEDS_REVIEW"
		}
		prop := fmt.Sprintf("batch-%d", i)
		var pid, tid string
		if err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,note,confidence,proposal_status) VALUES($1,$2,$3,$4,$5,'IDR',$6,NULLIF($7,''),$8,NULLIF($9,''),NULLIF($10,''),1,$11) RETURNING id`, householdID, sourceEventID, prop, v.Type, v.Amount, v.TransactionAt, v.Merchant, cat, v.Description, v.Note, proposalStatus).Scan(&pid); err != nil {
			return outcome, err
		}
		if err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,note,counterparty_name,source_confidence,classification_confidence,created_by_user_id,confirmed_at,merchant_id) VALUES($1,$2,$3,$4,'IDR',$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),1,1,$10,CASE WHEN $3::text='CONFIRMED' THEN now() END,NULLIF($11::text,'')::uuid) RETURNING id`, householdID, v.Type, status, v.Amount, v.TransactionAt, cat, v.Description, v.Note, v.Merchant, userID, merchantID).Scan(&tid); err != nil {
			return outcome, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CONFIRM_PENDING_BATCH','transaction',$3,jsonb_build_object('batch_id',$4::uuid,'item_index',$5::integer,'status',$6::text,'similar_existing',$7::boolean))`, householdID, userID, tid, batchID, i, status, v.SimilarExisting != ""); err != nil {
			return outcome, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_TEXT',1,jsonb_build_object('proposal_id',$3::uuid))`, tid, sourceEventID, pid); err != nil {
			return outcome, err
		}
		if status == "CONFIRMED" {
			outcome.Confirmed++
			if err = reviewdomain.RefreshOpenCycleResiduals(ctx, tx, householdID, v.TransactionAt, userID); err != nil {
				return outcome, err
			}
			continue
		}
		outcome.NeedsReview++
		if err = EnqueueReviewRequest(ctx, tx, tid, "AMBIGUOUS_CATEGORY", update.Message.Chat.ID, update.Message.MessageID, ReviewQuestion(v.Amount, batchItemLabel(v))); err != nil {
			return outcome, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET status='CONFIRMED',resolved_at=now() WHERE id=$1`, batchID); err != nil {
		return outcome, err
	}
	return outcome, nil
}

func batchItemLabel(item agentPendingBatchItem) string {
	if strings.TrimSpace(item.Merchant) != "" {
		return item.Merchant
	}
	return item.Description
}

// batchOutcomeMessage is the deterministic reply for a confirmed batch.
func batchOutcomeMessage(recorded, needsReview, skipped int) string {
	parts := []string{}
	switch {
	case recorded > 0:
		parts = append(parts, fmt.Sprintf("✅ %d transaksi sudah tercatat.", recorded))
	case needsReview > 0:
		parts = append(parts, "Daftar sudah kucatat, tapi belum ada yang final.")
	}
	if needsReview > 0 {
		parts = append(parts, fmt.Sprintf("%d lagi belum punya kategori — pilih di kartu di bawah, ya.", needsReview))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d sudah tercatat dari dokumen yang kamu kirim, jadi tidak dicatat lagi.", skipped))
	}
	if len(parts) == 0 {
		return "Tidak ada transaksi baru yang dicatat dari daftar ini."
	}
	return strings.Join(parts, " ")
}

// batchStagedMessage lists the staged items so the household confirms what it
// can see, including which items will wait for a category and which look like
// a transaction already recorded.
func batchStagedMessage(mutation map[string]any, header string) string {
	items, _ := mutation["items"].([]map[string]any)
	var b strings.Builder
	b.WriteString(header)
	for index, item := range items {
		amount, _ := item["amount_idr"].(string)
		label, _ := item["merchant"].(string)
		if strings.TrimSpace(label) == "" {
			label, _ = item["description"].(string)
		}
		if strings.TrimSpace(label) == "" {
			label = "Transaksi"
		}
		line := fmt.Sprintf("\n%d. %s · Rp%s", index+1, label, FormatIDR(amount))
		if typ, _ := item["type"].(string); typ == "INCOME" {
			line += " (pemasukan)"
		} else if slug, _ := item["category_slug"].(string); slug == "" {
			line += " · kategori belum ada, nanti ditinjau"
		}
		if similar, _ := item["similar_existing"].(string); similar != "" {
			line += "\n   ⚠️ mirip yang sudah tercatat: " + similar
		}
		if already, _ := item["already_recorded"].(bool); already {
			line += "\n   sudah tercatat dari dokumenmu, tidak akan dicatat lagi"
		}
		b.WriteString(line)
	}
	if total, _ := mutation["total_idr"].(string); total != "" && len(items) > 1 {
		b.WriteString("\n\nTotal: Rp" + FormatIDR(total))
	}
	b.WriteString("\n\nPilih tombol konfirmasi kalau sudah benar, atau sebutkan item yang ingin diubah. Daftar ini menunggu 30 menit.")
	return b.String()
}
