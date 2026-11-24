package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/merchantmemory"
)

var selfEnteredSourceTypes = []string{"TELEGRAM_TEXT", "TELEGRAM_CALLBACK", "WEB_MANUAL"}

func selfEnteredTransaction(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, householdID, transactionID string) (bool, error) {
	var self bool
	err := q.QueryRow(ctx, `SELECT t.type IN ('INCOME','EXPENSE') AND NOT EXISTS (
			SELECT 1 FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id
			WHERE te.transaction_id=t.id AND NOT s.source_type=ANY($3::text[]))
		FROM transaction t WHERE t.id=$1 AND t.household_id=$2`, transactionID, householdID, selfEnteredSourceTypes).Scan(&self)
	return self, err
}

func (p *Processor) agentStageCorrection(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	targetRef, _ := args["target_ref"].(string)
	search, _ := args["search_text"].(string)
	category, _ := args["category_slug"].(string)
	description, _ := args["description"].(string)
	amount, _ := args["amount_idr"].(string)
	merchant := clean(fmt.Sprint(valueOr(args["merchant"], "")), 160)
	var transactionID string
	if targetRef != "" {
		id, err := p.resolveTransactionReference(ctx, state.HouseholdID, state.Update, targetRef)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Status = "TARGET_UNAVAILABLE"
			result.Facts = map[string]any{"target_ref": targetRef}
			return result, true, nil
		}
		if err != nil {
			return result, true, err
		}
		transactionID = id
	} else {
		if strings.TrimSpace(search) == "" {
			result.Status = "MISSING_TARGET"
			return result, true, nil
		}
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args)
		if recoverable, ok := recoverableRange(err); ok {
			return result, true, recoverable
		}
		if err != nil {
			return result, true, err
		}
		rows, err := p.pool.Query(ctx, `SELECT t.id FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status<>'VOIDED' AND t.type IN('INCOME','EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 AND (t.counterparty_name ILIKE '%'||$4::text||'%' OR t.description ILIKE '%'||$4::text||'%' OR c.name ILIKE '%'||$4::text||'%') ORDER BY t.transaction_at DESC LIMIT 2`, state.HouseholdID, r.From, r.To, search)
		if err != nil {
			return result, true, err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return result, true, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return result, true, err
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
			return result, true, recoverableDate(err)
		}
		proposedAt = &at
	}
	if categoryID == nil && strings.TrimSpace(description) == "" && proposedAt == nil && amount == "" && merchant == "" {
		result.Status = "MISSING_CHANGE"
		return result, true, nil
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var label, currentAmount, currentMerchant, currentStatus, currentType string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(counterparty_name,description,'Transaksi'),amount::text,COALESCE(counterparty_name,''),status,type FROM transaction WHERE id=$1 AND household_id=$2 AND status<>'VOIDED' FOR UPDATE`, transactionID, state.HouseholdID).Scan(&label, &currentAmount, &currentMerchant, &currentStatus, &currentType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			result.Status = "TARGET_UNAVAILABLE"
			return result, true, nil
		}
		return result, true, err
	}
	blocker, err := correctionBlocker(ctx, tx, state.HouseholdID, transactionID, currentType, currentStatus, categoryID != nil)
	if err != nil {
		return result, true, err
	}
	if blocker != "" {
		result.Status = blocker
		result.Facts = map[string]any{"label": untrustedField(label)}
		return result, true, nil
	}
	if amount == currentAmount {
		amount = ""
	}
	if merchant == currentMerchant {
		merchant = ""
	}
	if categoryID == nil && strings.TrimSpace(description) == "" && proposedAt == nil && amount == "" && merchant == "" {
		result.Status = "MISSING_CHANGE"
		return result, true, nil
	}
	if amount != "" || merchant != "" {
		self, err := selfEnteredTransaction(ctx, tx, state.HouseholdID, transactionID)
		if err != nil {
			return result, true, err
		}
		if !self {
			result.Status = "SOURCE_EVIDENCE_FIXES_AMOUNT"
			result.Facts = map[string]any{"label": untrustedField(label), "amount_idr": currentAmount}
			return result, true, nil
		}
	}
	if err = tx.QueryRow(ctx, `INSERT INTO telegram_pending_action(household_id,telegram_user_id,telegram_chat_id,transaction_id,proposed_transaction_at,proposed_category_id,proposed_description,proposed_amount,proposed_merchant,status,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8::text,'')::numeric,NULLIF($9::text,''),'PENDING',now()+$10::interval) ON CONFLICT(telegram_user_id,telegram_chat_id) WHERE status='PENDING' DO UPDATE SET transaction_id=excluded.transaction_id,proposed_transaction_at=excluded.proposed_transaction_at,proposed_category_id=excluded.proposed_category_id,proposed_description=excluded.proposed_description,proposed_amount=excluded.proposed_amount,proposed_merchant=excluded.proposed_merchant,confirmation_token=gen_random_uuid(),expires_at=excluded.expires_at,created_at=now() RETURNING confirmation_token::text`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID, transactionID, proposedAt, categoryID, clean(description, 500), amount, merchant, pendingConfirmationWindow).Scan(&result.confirmationToken); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-correction-proposal',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	result.Status = "PENDING_CONFIRMATION"
	result.Mutation = map[string]any{"action": "CORRECTION_STAGED", "target_ref": targetRef, "label": label, "amount_idr": currentAmount, "category_slug": category, "description": description}
	if categoryID != nil {
		var name string
		if err = tx.QueryRow(ctx, `SELECT name FROM category WHERE id=$1`, *categoryID).Scan(&name); err != nil {
			return result, true, err
		}
		result.Mutation["category_name"] = name
	}
	if amount != "" {
		result.Mutation["new_amount_idr"] = amount
	}
	if merchant != "" {
		result.Mutation["new_merchant"] = merchant
	}
	if proposedAt != nil {
		result.Mutation["transaction_at"] = proposedAt.In(jakartaLocation()).Format(time.RFC3339)
	}
	if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	return result, false, nil
}

func valueOr(value any, fallback string) any {
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func (p *Processor) agentResolvePendingAction(ctx context.Context, state *agentState, call gateway.ToolCall, confirm bool) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var actionID string
	err = tx.QueryRow(ctx, `SELECT id FROM telegram_pending_action WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID).Scan(&actionID)
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
		outcome, err := applyPendingCorrectionTx(ctx, tx, state.HouseholdID, actionID, state.Update.Message.From.ID)
		if err != nil {
			return result, true, err
		}
		if outcome.Refused {
			status = "REFUSED"
			result.Status = "SOURCE_EVIDENCE_FIXES_AMOUNT"
		}
		if outcome.Voided {
			status = "REFUSED"
			result.Status = "TRANSACTION_VOIDED"
		}
		if outcome.Blocked != "" {
			status = "REFUSED"
			result.Status = outcome.Blocked
		}
	} else if _, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-correction-resolution',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if status != "REFUSED" {
		result.Status = status
		result.Mutation = map[string]any{"action": "CORRECTION_RESOLVED", "confirmed": confirm}
	}
	if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	return result, false, nil
}

type correctionOutcome struct {
	Refused    bool
	Voided     bool
	Blocked    string
	ProposedAt *time.Time
}

const (
	correctionUnderReview      = "TRANSACTION_UNDER_REVIEW"
	correctionTransferCategory = "TRANSFER_NOT_CATEGORIZABLE"
)

func correctionBlocker(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, householdID, transactionID, txType, txStatus string, setsCategory bool) (string, error) {
	if txStatus == "NEEDS_REVIEW" {
		return correctionUnderReview, nil
	}
	var open bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND status IN ('PENDING_SEND','OPEN'))`, householdID, transactionID).Scan(&open); err != nil {
		return "", err
	}
	if open {
		return correctionUnderReview, nil
	}
	if txType == "TRANSFER" && setsCategory {
		return correctionTransferCategory, nil
	}
	return "", nil
}

func applyPendingCorrectionTx(ctx context.Context, tx pgx.Tx, householdID, actionID string, telegramUserID int64) (correctionOutcome, error) {
	var outcome correctionOutcome
	var transactionID, txStatus, txType string
	var proposedCategoryID, proposedDescription, proposedAmount, proposedMerchant *string
	var proposedAt *time.Time
	var beforeAt time.Time
	var beforeAmount, beforeMerchant, beforeCategory, beforeDescription string
	if err := tx.QueryRow(ctx, `SELECT a.transaction_id,a.proposed_transaction_at,a.proposed_category_id::text,a.proposed_description,a.proposed_amount::text,a.proposed_merchant,
			t.status,t.type,t.transaction_at,t.amount::text,COALESCE(t.counterparty_name,''),COALESCE(t.category_id::text,''),COALESCE(t.description,'')
		FROM telegram_pending_action a JOIN transaction t ON t.id=a.transaction_id AND t.household_id=a.household_id
		WHERE a.id=$1 AND a.household_id=$2 FOR UPDATE OF a,t`, actionID, householdID).Scan(&transactionID, &proposedAt, &proposedCategoryID, &proposedDescription, &proposedAmount, &proposedMerchant,
		&txStatus, &txType, &beforeAt, &beforeAmount, &beforeMerchant, &beforeCategory, &beforeDescription); err != nil {
		return outcome, err
	}
	if txStatus == "VOIDED" {
		outcome.Voided = true
		_, err := tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
		return outcome, err
	}
	blocker, err := correctionBlocker(ctx, tx, householdID, transactionID, txType, txStatus, proposedCategoryID != nil)
	if err != nil {
		return outcome, err
	}
	if blocker != "" {
		outcome.Blocked = blocker
		_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
		return outcome, err
	}
	if proposedAmount != nil || proposedMerchant != nil {
		self, err := selfEnteredTransaction(ctx, tx, householdID, transactionID)
		if err != nil {
			return outcome, err
		}
		if !self {
			outcome.Refused = true
			_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
			return outcome, err
		}
	}
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, telegramUserID, householdID).Scan(&userID); err != nil {
		return outcome, fmt.Errorf("re-authorize Telegram identity: %w", err)
	}
	merchantID := ""
	if proposedMerchant != nil {
		id, _, err := resolveEntryMerchantTx(ctx, tx, householdID, *proposedMerchant)
		if err != nil {
			return outcome, err
		}
		merchantID = id
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction SET transaction_at=COALESCE($2,transaction_at),category_id=COALESCE($3::uuid,category_id),description=COALESCE(NULLIF($4::text,''),description),
			amount=COALESCE($5::numeric,amount),counterparty_name=COALESCE($6::text,counterparty_name),merchant_id=CASE WHEN $6::text IS NULL THEN merchant_id ELSE NULLIF($7::text,'')::uuid END,updated_at=now()
		WHERE id=$1 AND household_id=$8`, transactionID, proposedAt, proposedCategoryID, proposedDescription, proposedAmount, proposedMerchant, merchantID, householdID); err != nil {
		return outcome, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'TELEGRAM',$2,'EDIT_TRANSACTION','transaction',$3,
			jsonb_build_object('transaction_at',$4::timestamptz,'amount',$5::text,'counterparty_name',$6::text,'category_id',NULLIF($7::text,''),'description',$8::text),
			jsonb_build_object('transaction_at',$9::timestamptz,'category_id',$10::text,'description',$11::text,'amount',$12::text,'counterparty_name',$13::text,'pending_action_id',$14::uuid))`,
		householdID, userID, transactionID, beforeAt, beforeAmount, beforeMerchant, beforeCategory, beforeDescription,
		proposedAt, proposedCategoryID, proposedDescription, proposedAmount, proposedMerchant, actionID); err != nil {
		return outcome, err
	}
	if _, err := tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CONFIRMED',resolved_at=now() WHERE id=$1`, actionID); err != nil {
		return outcome, err
	}
	if txStatus == "CONFIRMED" && (proposedAmount != nil || proposedAt != nil) {
		if err := reviewdomain.RefreshOpenCycleResiduals(ctx, tx, householdID, beforeAt, userID); err != nil {
			return outcome, err
		}
		if proposedAt != nil && !proposedAt.Equal(beforeAt) {
			if err := reviewdomain.RefreshOpenCycleResiduals(ctx, tx, householdID, *proposedAt, userID); err != nil {
				return outcome, err
			}
		}
	}
	outcome.ProposedAt = proposedAt
	return outcome, nil
}

func resolveEntryMerchantTx(ctx context.Context, tx pgx.Tx, householdID, raw string) (string, *merchantmemory.Match, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, nil
	}
	match, err := merchantmemory.Lookup(ctx, tx, householdID, raw)
	if err != nil {
		return "", nil, err
	}
	if match != nil {
		return match.MerchantID, match, nil
	}
	var merchantID string
	err = tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id`, householdID, raw).Scan(&merchantID)
	return merchantID, nil, err
}
