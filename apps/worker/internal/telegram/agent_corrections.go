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

const statusFactsLocked = "TRANSACTION_FACTS_LOCKED"

func factsLockStatus(reason string) string {
	if reason == reviewdomain.FactsLockSource {
		return "SOURCE_EVIDENCE_FIXES_AMOUNT"
	}
	return statusFactsLocked
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
		reason, err := reviewdomain.HandEnteredFactsLock(ctx, tx, state.HouseholdID, transactionID)
		if err != nil {
			return result, true, err
		}
		if reason != "" {
			result.Status = factsLockStatus(reason)
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
			result.Status = factsLockStatus(outcome.RefusedReason)
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
	Refused       bool
	RefusedReason string
	Voided        bool
	Blocked       string
	ProposedAt    *time.Time
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
	if err := tx.QueryRow(ctx, `SELECT a.transaction_id,a.proposed_transaction_at,a.proposed_category_id::text,a.proposed_description,a.proposed_amount::text,a.proposed_merchant,
			t.status,t.type
		FROM telegram_pending_action a JOIN transaction t ON t.id=a.transaction_id AND t.household_id=a.household_id
		WHERE a.id=$1 AND a.household_id=$2 FOR UPDATE OF a,t`, actionID, householdID).Scan(&transactionID, &proposedAt, &proposedCategoryID, &proposedDescription, &proposedAmount, &proposedMerchant,
		&txStatus, &txType); err != nil {
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
		reason, err := reviewdomain.HandEnteredFactsLock(ctx, tx, householdID, transactionID)
		if err != nil {
			return outcome, err
		}
		if reason != "" {
			outcome.Refused, outcome.RefusedReason = true, reason
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
		merchantReview, err := reviewdomain.MerchantLinkNeedsReview(ctx, tx, householdID, *proposedMerchant)
		if err != nil {
			return outcome, err
		}
		if merchantReview {
			outcome.Blocked = "AMBIGUOUS_MERCHANT"
			_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
			return outcome, err
		}
		id, _, err := resolveEntryMerchantTx(ctx, tx, householdID, *proposedMerchant)
		if err != nil {
			return outcome, err
		}
		merchantID = id
	}
	description := ""
	if proposedDescription != nil {
		description = *proposedDescription
	}
	amount := ""
	if proposedAmount != nil {
		amount = *proposedAmount
	}
	categoryID := ""
	if proposedCategoryID != nil {
		categoryID = *proposedCategoryID
	}
	err = reviewdomain.CorrectConfirmedTransaction(ctx, tx, reviewdomain.CorrectionCommand{
		HouseholdID: householdID, ActorUserID: userID, ActorType: "TELEGRAM", TransactionID: transactionID, PendingActionID: actionID,
		CategoryID: categoryID, Description: description, AmountIDR: amount, CounterpartyName: proposedMerchant, MerchantID: merchantID,
		TransactionAt: proposedAt, AllowAnyType: true,
	})
	switch {
	case errors.Is(err, reviewdomain.ErrCorrectionDateFuture), errors.Is(err, reviewdomain.ErrCorrectionDateTooOld):
		outcome.Blocked = "INVALID_TIME"
		_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
		return outcome, err
	case errors.Is(err, reviewdomain.ErrInvalidCandidate):
		outcome.Blocked = "INVALID_CATEGORY"
		_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
		return outcome, err
	case errors.Is(err, reviewdomain.ErrCorrectionAmountShape):
		outcome.Blocked = "INVALID_AMOUNT"
		_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
		return outcome, err
	case errors.Is(err, reviewdomain.ErrCorrectionNotAllowed):
		var locked reviewdomain.CorrectionLockedError
		if errors.As(err, &locked) {
			outcome.Refused, outcome.RefusedReason = true, locked.Reason
		} else {
			outcome.Blocked = "TARGET_UNAVAILABLE"
		}
		_, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID)
		return outcome, err
	case err != nil && !errors.Is(err, reviewdomain.ErrCorrectionEmpty):
		return outcome, err
	}
	if _, err := tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CONFIRMED',resolved_at=now() WHERE id=$1`, actionID); err != nil {
		return outcome, err
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
	merchantID, err := reviewdomain.ResolveOrCreateMerchant(ctx, tx, householdID, raw)
	if errors.Is(err, reviewdomain.ErrAmbiguousMerchantMatch) {
		return "", nil, nil
	}
	return merchantID, nil, err
}
