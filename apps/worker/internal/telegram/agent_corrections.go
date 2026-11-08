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

// selfEnteredSourceTypes are the sources whose facts the household itself
// typed. Only a transaction whose every evidence row comes from one of them may
// have its amount or merchant corrected from chat: for a bank, provider or
// document-backed transaction the source evidence fixes those facts.
var selfEnteredSourceTypes = []string{"TELEGRAM_TEXT", "TELEGRAM_CALLBACK", "WEB_MANUAL"}

// selfEnteredTransaction reports whether a transaction's facts come only from
// the household's own entries. It runs inside the caller's transaction so the
// check and the change see the same evidence.
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
	var label, currentAmount, currentMerchant string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(counterparty_name,description,'Transaksi'),amount::text,COALESCE(counterparty_name,'') FROM transaction WHERE id=$1 AND household_id=$2 AND status<>'VOIDED' FOR UPDATE`, transactionID, state.HouseholdID).Scan(&label, &currentAmount, &currentMerchant); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			result.Status = "TARGET_UNAVAILABLE"
			return result, true, nil
		}
		return result, true, err
	}
	if amount != "" || merchant != "" {
		self, err := selfEnteredTransaction(ctx, tx, state.HouseholdID, transactionID)
		if err != nil {
			return result, true, err
		}
		if !self {
			// Never staged: the amount/merchant of a bank, provider or document
			// record comes from its source evidence, not from chat.
			result.Status = "SOURCE_EVIDENCE_FIXES_AMOUNT"
			result.Facts = map[string]any{"label": untrustedField(label), "amount_idr": currentAmount}
			return result, true, nil
		}
	}
	if amount == currentAmount {
		amount = ""
	}
	if merchant == currentMerchant {
		merchant = ""
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
	// Refused means the staged amount/merchant change no longer applies: the
	// transaction gained source evidence after it was staged.
	Refused bool
	// Voided means the transaction was voided after the correction was staged,
	// so nothing is edited: a voided row is no longer the household's to change.
	Voided bool
	// ProposedAt is the new transaction time, when the correction moved it.
	ProposedAt *time.Time
}

// applyPendingCorrectionTx applies one locked, still-pending correction and
// resolves it, with an audit row holding the before and after values. The
// self-entered rule is checked again here, under the transaction lock, so a
// receipt merged in after staging wins over the chat change. A confirmed
// change refreshes the open cycle residuals of both the old and new time.
func applyPendingCorrectionTx(ctx context.Context, tx pgx.Tx, householdID, actionID string, telegramUserID int64) (correctionOutcome, error) {
	var outcome correctionOutcome
	var transactionID, txStatus string
	var proposedCategoryID, proposedDescription, proposedAmount, proposedMerchant *string
	var proposedAt *time.Time
	var beforeAt time.Time
	var beforeAmount, beforeMerchant, beforeCategory, beforeDescription string
	if err := tx.QueryRow(ctx, `SELECT a.transaction_id,a.proposed_transaction_at,a.proposed_category_id::text,a.proposed_description,a.proposed_amount::text,a.proposed_merchant,
			t.status,t.transaction_at,t.amount::text,COALESCE(t.counterparty_name,''),COALESCE(t.category_id::text,''),COALESCE(t.description,'')
		FROM telegram_pending_action a JOIN transaction t ON t.id=a.transaction_id AND t.household_id=a.household_id
		WHERE a.id=$1 AND a.household_id=$2 FOR UPDATE OF a,t`, actionID, householdID).Scan(&transactionID, &proposedAt, &proposedCategoryID, &proposedDescription, &proposedAmount, &proposedMerchant,
		&txStatus, &beforeAt, &beforeAmount, &beforeMerchant, &beforeCategory, &beforeDescription); err != nil {
		return outcome, err
	}
	if txStatus == "VOIDED" {
		outcome.Voided = true
		_, err := tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
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

// offerExistingEditForTurn runs the single entry's "is this the one you already
// recorded?" check and, when it stages an edit, queues the question in the same
// transaction.
func (p *Processor) offerExistingEditForTurn(ctx context.Context, state *agentState, value validatedExtraction, result *agentToolResult) (string, error) {
	label, existingAt, found, err := p.similarRecentExpense(ctx, state.HouseholdID, value.Amount, value.Merchant, value.TransactionAt)
	if err != nil || !found {
		return "", err
	}
	_ = existingAt
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var transactionID string
	if err := tx.QueryRow(ctx, `SELECT id FROM transaction WHERE household_id=$1 AND status<>'VOIDED' AND type='EXPENSE' AND amount=$2::numeric AND transaction_at>=now()-interval '7 days' AND (counterparty_name ILIKE '%'||$3::text||'%' OR description ILIKE '%'||$3::text||'%') ORDER BY transaction_at DESC LIMIT 1`, state.HouseholdID, value.Amount, value.Merchant).Scan(&transactionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	var token string
	if err = tx.QueryRow(ctx, `INSERT INTO telegram_pending_action(household_id,telegram_user_id,telegram_chat_id,transaction_id,proposed_transaction_at,status,expires_at) VALUES($1,$2,$3,$4,$5,'PENDING',now()+$6::interval) ON CONFLICT(telegram_user_id,telegram_chat_id) WHERE status='PENDING' DO UPDATE SET transaction_id=excluded.transaction_id,proposed_transaction_at=excluded.proposed_transaction_at,proposed_category_id=NULL,proposed_description=NULL,proposed_amount=NULL,proposed_merchant=NULL,confirmation_token=gen_random_uuid(),expires_at=excluded.expires_at,created_at=now() RETURNING confirmation_token::text`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID, transactionID, value.TransactionAt, pendingConfirmationWindow).Scan(&token); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-edit-proposal',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return "", err
	}
	result.confirmationToken = token
	result.Status = "EDIT_CONFIRMATION_REQUIRED"
	result.Mutation = map[string]any{"action": "POSSIBLE_EXISTING_TRANSACTION", "label": label, "amount_idr": value.Amount, "transaction_at": value.TransactionAt.In(jakartaLocation()).Format(time.RFC3339)}
	if err = p.queueTurnReplyTx(ctx, tx, state, *result); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

// resolveEntryMerchantTx gives a typed merchant the household's merchant
// identity, exactly as a review answer does (agentSaveReviewField): a
// household-confirmed alias wins, otherwise the normalized merchant row is
// created or reused. The remembered match carries the alias's category.
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
