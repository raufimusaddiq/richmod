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
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) executeAgentSideEffect(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, metadata gateway.Metadata) (agentToolResult, bool, error) {
	switch call.Name {
	case "record_transaction":
		return p.agentRecordTransaction(ctx, state, call, args, metadata)
	case "record_transaction_batch":
		return p.agentStageBatch(ctx, state, call, args)
	case "propose_transaction_correction":
		return p.agentStageCorrection(ctx, state, call, args)
	case "confirm_pending_action":
		return p.agentResolvePendingAction(ctx, state, call, true)
	case "cancel_pending_action":
		return p.agentResolvePendingAction(ctx, state, call, false)
	case "confirm_pending_batch":
		return p.agentResolvePendingBatch(ctx, state, call, true)
	case "cancel_pending_batch":
		return p.agentResolvePendingBatch(ctx, state, call, false)
	case "update_pending_batch":
		return p.agentUpdatePendingBatch(ctx, state, call, args)
	default:
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, false, fmt.Errorf("unsupported core side-effect tool %q", call.Name)
	}
}

func (p *Processor) agentRecordTransaction(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, metadata gateway.Metadata) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	value, err := nativeValidatedExtraction(args, state.Now)
	if err != nil {
		return result, true, fmt.Errorf("invalid transaction proposal: %w", err)
	}

	// Creating a new transaction must never silently turn into a correction of a
	// similar recent transaction. Corrections require the dedicated correction
	// tool and an opaque/uniquely resolved target.
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)

	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, fmt.Errorf("re-authorize Telegram identity: %w", err)
	}

	var categoryID *string
	if value.CategorySlug != "" {
		var id string
		err = tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, state.HouseholdID, value.CategorySlug).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Status = "INVALID_CATEGORY"
			result.Facts = map[string]any{"category_slug": value.CategorySlug}
			return result, true, nil
		}
		if err != nil {
			return result, true, fmt.Errorf("validate category: %w", err)
		}
		categoryID = &id
	}

	autoConfirm := shouldAutoConfirmTransaction(value, categoryID != nil)
	proposalStatus, transactionStatus := "NEEDS_REVIEW", "NEEDS_REVIEW"
	if autoConfirm {
		proposalStatus, transactionStatus = "ACCEPTED", "CONFIRMED"
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"gateway_model":       metadata.Model,
		"input_tokens":        metadata.InputTokens,
		"output_tokens":       metadata.OutputTokens,
		"cost":                metadata.Cost,
		"category_confidence": value.CategoryConfidence,
		"time_precision":      value.TimePrecision,
		"time_period":         value.TimePeriod,
		"agent_sprint":        1,
	})
	var proposalID string
	if err = tx.QueryRow(ctx, `
		INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,note,confidence,proposal_status,metadata_json)
		VALUES($1,$2,$3,$4,'IDR',$5,NULLIF($6,''),$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12::jsonb)
		RETURNING id`, state.HouseholdID, state.SourceEventID, value.Type, value.Amount, value.TransactionAt, value.Merchant, categoryID, value.Description, value.Note, value.Confidence, proposalStatus, string(metadataJSON)).Scan(&proposalID); err != nil {
		return result, true, fmt.Errorf("create transaction proposal: %w", err)
	}
	var transactionID string
	if err = tx.QueryRow(ctx, `
		INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,note,counterparty_name,source_confidence,classification_confidence,created_by_user_id,confirmed_at)
		VALUES($1,$2,$3,$4,'IDR',$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12,CASE WHEN $3='CONFIRMED' THEN now() END)
		RETURNING id`, state.HouseholdID, value.Type, transactionStatus, value.Amount, value.TransactionAt, categoryID, value.Description, value.Note, value.Merchant, value.Confidence, value.CategoryConfidence, userID).Scan(&transactionID); err != nil {
		return result, true, fmt.Errorf("create reviewed transaction: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_TEXT',$3,jsonb_build_object('proposal_id',$4::uuid))`, transactionID, state.SourceEventID, value.Confidence, proposalID); err != nil {
		return result, true, err
	}
	refs, err := persistAgentTransactionReferencesTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, fmt.Sprintf("p%dr0", state.ModelPhases), []string{transactionID})
	if err != nil {
		return result, true, fmt.Errorf("persist transaction reference: %w", err)
	}
	if len(refs) != 1 {
		return result, true, fmt.Errorf("persist transaction reference: expected one ref, got %d", len(refs))
	}
	sourceStatus := "NEEDS_REVIEW"
	if autoConfirm {
		sourceStatus = "PROCESSED"
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status=$2,parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID, sourceStatus); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','CREATE_FROM_TELEGRAM','transaction',$2,jsonb_build_object('status',$3::text,'proposal_id',$4::uuid,'agent_sprint',1))`, state.HouseholdID, transactionID, transactionStatus, proposalID); err != nil {
		return result, true, err
	}
	if !autoConfirm {
		reviewType := "AMBIGUOUS_CATEGORY"
		if value.Merchant == "" {
			reviewType = "UNKNOWN_MERCHANT"
		}
		if err = EnqueueReviewRequest(ctx, tx, transactionID, reviewType, state.Update.Message.Chat.ID, state.Update.Message.MessageID, ReviewQuestion(value.Amount, value.Merchant)); err != nil {
			return result, true, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}

	result.Status = transactionStatus
	result.References = refs
	result.Mutation = map[string]any{
		"action":         "TRANSACTION_RECORDED",
		"ref":            refs[0].Ref,
		"type":           value.Type,
		"amount_idr":     value.Amount,
		"merchant":       value.Merchant,
		"category_slug":  value.CategorySlug,
		"description":    value.Description,
		"transaction_at": value.TransactionAt.In(jakartaLocation()).Format(time.RFC3339),
		"time_precision": value.TimePrecision,
		"status":         transactionStatus,
	}
	if !autoConfirm {
		result.Review = map[string]any{"required": true, "reason": "TRANSACTION_NEEDS_REVIEW"}
	}
	return result, true, nil
}

func (p *Processor) agentStageBatch(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	raw, ok := args["items"].([]any)
	if !ok || len(raw) < 1 || len(raw) > 10 {
		return result, true, fmt.Errorf("invalid batch")
	}
	type pending struct {
		Type, Amount, Merchant, CategorySlug, Description string
		TransactionAt                                    time.Time
	}
	vals := make([]pending, 0, len(raw))
	total := big.NewInt(0)
	public := make([]map[string]any, 0, len(raw))
	for i, entryRaw := range raw {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return result, true, fmt.Errorf("invalid batch item")
		}
		v, err := nativeValidatedExtraction(entry, state.Now)
		if err != nil {
			return result, true, err
		}
		n, _ := new(big.Int).SetString(v.Amount, 10)
		total.Add(total, n)
		vals = append(vals, pending{Type: v.Type, Amount: v.Amount, Merchant: v.Merchant, CategorySlug: v.CategorySlug, Description: v.Description, TransactionAt: v.TransactionAt})
		public = append(public, map[string]any{"item_ref": fmt.Sprintf("batch_%d", i+1), "type": v.Type, "amount_idr": v.Amount, "merchant": v.Merchant, "category_slug": v.CategorySlug, "description": v.Description, "transaction_at": v.TransactionAt.In(jakartaLocation()).Format(time.RFC3339)})
	}

	encoded, _ := json.Marshal(vals)
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	for _, item := range vals {
		if item.CategorySlug == "" {
			continue
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM category WHERE household_id=$1 AND slug=$2 AND active)`, state.HouseholdID, item.CategorySlug).Scan(&exists); err != nil {
			return result, true, err
		}
		if !exists {
			result.Status = "INVALID_CATEGORY"
			result.Facts = map[string]any{"category_slug": item.CategorySlug}
			return result, true, nil
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status) VALUES($1,$2,$3,$4,$5::jsonb,'PENDING') ON CONFLICT(telegram_user_id,telegram_chat_id) WHERE status='PENDING' DO UPDATE SET source_event_id=excluded.source_event_id,items_json=excluded.items_json,expires_at=now()+interval '5 minutes',created_at=now()`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID, state.SourceEventID, string(encoded)); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-batch-proposal',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "PENDING_CONFIRMATION"
	result.Mutation = map[string]any{"action": "BATCH_STAGED", "items": public, "count": len(public), "total_idr": total.String()}
	return result, true, nil
}

func (p *Processor) agentStageCorrection(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	targetRef, _ := args["target_ref"].(string)
	search, _ := args["search_text"].(string)
	category, _ := args["category_slug"].(string)
	description, _ := args["description"].(string)
	var transactionID string
	if targetRef != "" {
		id, err := p.resolveTransactionReference(ctx, state.HouseholdID, state.Update, targetRef)
		if err != nil {
			result.Status = "TARGET_UNAVAILABLE"
			result.Facts = map[string]any{"target_ref": targetRef}
			return result, true, nil
		}
		transactionID = id
	} else {
		if strings.TrimSpace(search) == "" {
			result.Status = "MISSING_TARGET"
			return result, true, nil
		}
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args)
		if err != nil {
			return result, true, err
		}
		rows, err := p.pool.Query(ctx, `SELECT t.id FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status<>'VOIDED' AND t.type IN('INCOME','EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 AND (t.counterparty_name ILIKE '%'||$4||'%' OR t.description ILIKE '%'||$4||'%' OR c.name ILIKE '%'||$4||'%') ORDER BY t.transaction_at DESC LIMIT 2`, state.HouseholdID, r.From, r.To, search)
		if err != nil {
			return result, true, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return result, true, err
			}
			ids = append(ids, id)
		}
		if len(ids) != 1 {
			result.Status = "AMBIGUOUS_TARGET"
			result.Facts = map[string]any{"match_count": len(ids), "search_text": search}
			return result, true, nil
		}
		transactionID = ids[0]
	}

	var categoryID *string
	if category != "" {
		var id string
		if err := p.pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, state.HouseholdID, category).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				result.Status = "INVALID_CATEGORY"
				return result, true, nil
			}
			return result, true, err
		}
		categoryID = &id
	}
	var proposedAt *time.Time
	if reference, ok := args["date_reference"].(string); ok && reference != "" {
		explicit, _ := args["explicit_date"].(string)
		local, _ := args["local_time"].(string)
		at, err := resolveTime(state.Now, &reference, stringPtr(explicit), stringPtr(local))
		if err != nil {
			return result, true, err
		}
		proposedAt = &at
	}
	if categoryID == nil && strings.TrimSpace(description) == "" && proposedAt == nil {
		result.Status = "MISSING_CHANGE"
		return result, true, nil
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var label, amount string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(counterparty_name,description,'Transaksi'),amount::text FROM transaction WHERE id=$1 AND household_id=$2 AND status<>'VOIDED' FOR UPDATE`, transactionID, state.HouseholdID).Scan(&label, &amount); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO telegram_pending_action(household_id,telegram_user_id,telegram_chat_id,transaction_id,proposed_transaction_at,proposed_category_id,proposed_description,status) VALUES($1,$2,$3,$4,$5,$6,$7,'PENDING') ON CONFLICT(telegram_user_id,telegram_chat_id) WHERE status='PENDING' DO UPDATE SET transaction_id=excluded.transaction_id,proposed_transaction_at=excluded.proposed_transaction_at,proposed_category_id=excluded.proposed_category_id,proposed_description=excluded.proposed_description,expires_at=now()+interval '5 minutes',created_at=now()`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID, transactionID, proposedAt, categoryID, clean(description, 500)); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-correction-proposal',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "PENDING_CONFIRMATION"
	result.Mutation = map[string]any{"action": "CORRECTION_STAGED", "target_ref": targetRef, "label": label, "amount_idr": amount, "category_slug": category, "description": description}
	if proposedAt != nil {
		result.Mutation["transaction_at"] = proposedAt.In(jakartaLocation()).Format(time.RFC3339)
	}
	return result, true, nil
}

func (p *Processor) agentResolvePendingAction(ctx context.Context, state *agentState, call gateway.ToolCall, confirm bool) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var actionID, transactionID string
	var proposedAt *time.Time
	var proposedCategoryID, proposedDescription *string
	err = tx.QueryRow(ctx, `SELECT id,transaction_id,proposed_transaction_at,proposed_category_id,proposed_description FROM telegram_pending_action WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID).Scan(&actionID, &transactionID, &proposedAt, &proposedCategoryID, &proposedDescription)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "NO_PENDING_ACTION"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	status := "CANCELLED"
	if confirm {
		status = "CONFIRMED"
		if _, err = tx.Exec(ctx, `UPDATE transaction SET transaction_at=COALESCE($2,transaction_at),category_id=COALESCE($3,category_id),description=COALESCE(NULLIF($4,''),description),updated_at=now() WHERE id=$1 AND household_id=$5`, transactionID, proposedAt, proposedCategoryID, proposedDescription, state.HouseholdID); err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) SELECT $1,'TELEGRAM',ti.user_id,'EDIT_TRANSACTION','transaction',$2,jsonb_build_object('transaction_at',$3::timestamptz,'category_id',$4::uuid,'description',$5) FROM telegram_identity ti WHERE ti.telegram_user_id=$6 AND ti.household_id=$1 AND ti.active`, state.HouseholdID, transactionID, proposedAt, proposedCategoryID, proposedDescription, state.Update.Message.From.ID); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status=$2,resolved_at=now() WHERE id=$1`, actionID, status); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-correction-resolution',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = status
	result.Mutation = map[string]any{"action": "CORRECTION_RESOLVED", "confirmed": confirm}
	return result, true, nil
}

func (p *Processor) agentResolvePendingBatch(ctx context.Context, state *agentState, call gateway.ToolCall, confirm bool) (agentToolResult, bool, error) {
	return p.agentFinalizePendingBatch(ctx, state, call, confirm, nil)
}

func (p *Processor) agentUpdatePendingBatch(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	ref, _ := args["item_ref"].(string)
	indexText := strings.TrimPrefix(ref, "batch_")
	index, err := strconv.Atoi(indexText)
	if err != nil || index < 1 {
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, true, fmt.Errorf("invalid item_ref")
	}
	confirm, _ := args["confirm_after_update"].(bool)
	return p.agentFinalizePendingBatch(ctx, state, call, confirm, &pendingBatchPatch{
		Index:        index - 1,
		Amount:       agentOptionalString(args["amount_idr"]),
		Merchant:     agentOptionalString(args["merchant"]),
		CategorySlug: agentOptionalString(args["category_slug"]),
		Description:  agentOptionalString(args["description"]),
	})
}

type pendingBatchPatch struct {
	Index                                      int
	Amount, Merchant, CategorySlug, Description *string
}

type agentPendingBatchItem struct {
	Type         string    `json:"Type"`
	Amount       string    `json:"Amount"`
	Merchant     string    `json:"Merchant"`
	CategorySlug string    `json:"CategorySlug"`
	Description  string    `json:"Description"`
	TransactionAt time.Time `json:"TransactionAt"`
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
			result.Status = "INVALID_ITEM_REF"
			return result, true, nil
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
					result.Status = "INVALID_CATEGORY"
					result.Facts = map[string]any{"category_slug": item.CategorySlug}
					return result, true, nil
				}
			}
		}
		if patch.Description != nil {
			item.Description = clean(*patch.Description, 500)
		}
		encoded, _ := json.Marshal(items)
		if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET items_json=$2::jsonb,expires_at=now()+interval '5 minutes' WHERE id=$1`, batchID, string(encoded)); err != nil {
			return result, true, err
		}
	}
	if !confirm {
		if patch == nil {
			if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET status='CANCELLED',resolved_at=now() WHERE id=$1`, batchID); err != nil {
				return result, true, err
			}
			result.Status = "CANCELLED"
		} else {
			result.Status = "UPDATED"
		}
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-batch-resolution',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
			return result, true, err
		}
		if err = tx.Commit(ctx); err != nil {
			return result, true, err
		}
		result.Mutation = map[string]any{"action": "BATCH_UPDATED", "confirmed": false, "count": len(items)}
		return result, true, nil
	}

	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	for i, v := range items {
		n, ok := new(big.Int).SetString(v.Amount, 10)
		if !ok || n.Sign() <= 0 || n.String() != v.Amount {
			return result, true, fmt.Errorf("invalid batch amount")
		}
		var cat *string
		if v.CategorySlug != "" {
			var cid string
			err = tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, state.HouseholdID, v.CategorySlug).Scan(&cid)
			if errors.Is(err, pgx.ErrNoRows) {
				result.Status = "INVALID_CATEGORY"
				result.Facts = map[string]any{"item_ref": fmt.Sprintf("batch_%d", i+1), "category_slug": v.CategorySlug}
				return result, true, nil
			}
			if err != nil {
				return result, true, err
			}
			cat = &cid
		}
		prop := fmt.Sprintf("batch-%d", i)
		var pid, tid string
		if err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,confidence,proposal_status) VALUES($1,(SELECT source_event_id FROM telegram_pending_batch WHERE id=$2),$3,$4,$5,'IDR',$6,NULLIF($7,''),$8,NULLIF($9,''),1,'ACCEPTED') RETURNING id`, state.HouseholdID, batchID, prop, v.Type, v.Amount, v.TransactionAt, v.Merchant, cat, v.Description).Scan(&pid); err != nil {
			return result, true, err
		}
		if err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,counterparty_name,source_confidence,classification_confidence,created_by_user_id,confirmed_at) VALUES($1,$2,'CONFIRMED',$3,'IDR',$4,$5,NULLIF($6,''),NULLIF($7,''),1,1,$8,now()) RETURNING id`, state.HouseholdID, v.Type, v.Amount, v.TransactionAt, cat, v.Description, v.Merchant, userID).Scan(&tid); err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,(SELECT source_event_id FROM telegram_pending_batch WHERE id=$2),'TELEGRAM_TEXT',1,jsonb_build_object('proposal_id',$3::uuid))`, tid, batchID, pid); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET status='CONFIRMED',resolved_at=now() WHERE id=$1`, batchID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-batch-confirmation',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "CONFIRMED"
	result.Mutation = map[string]any{"action": "BATCH_RECORDED", "confirmed": true, "count": len(items)}
	return result, true, nil
}
