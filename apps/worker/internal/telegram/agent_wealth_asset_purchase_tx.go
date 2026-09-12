package telegram

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// agentResolveBoundWealthAssetPurchaseAtomic performs the complete logical
// Wealth -> ASSET_PURCHASE transition in one PostgreSQL transaction. No
// canonical transfer is allowed to commit before the bound Wealth review is
// dismissed/resolved.
func (p *Processor) agentResolveBoundWealthAssetPurchaseAtomic(
	ctx context.Context,
	state *agentState,
	call gateway.ToolCall,
	args map[string]any,
	binding *agentReviewBinding,
) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	sourceHint, _ := args["source_account_hint"].(string)
	wealthHint, _ := args["wealth_account_hint"].(string)
	amount, _ := args["amount_idr"].(string)
	atText, _ := args["transaction_at"].(string)
	if strings.TrimSpace(sourceHint) == "" || !validReviewTimestamp(atText) {
		result.Status = "MISSING_ASSET_PURCHASE_FACTS"
		result.Review = map[string]any{"required": true, "missing_fields": []string{"source_account_hint", "transaction_at"}}
		return result, true, nil
	}
	at, err := time.Parse(time.RFC3339, atText)
	if err != nil {
		result.Status = "MISSING_ASSET_PURCHASE_FACTS"
		return result, true, nil
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)

	resolvedWealthID, institution, observationHint, originalSource, valid, err := p.loadBoundWealthObservationTx(ctx, tx, state, binding)
	if err != nil {
		return result, true, err
	}
	if !valid {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}

	accountID, err := resolveUniqueAccountHint(ctx, tx, state.HouseholdID, sourceHint)
	if err != nil {
		result.Status = "ACCOUNT_AMBIGUOUS"
		return result, true, nil
	}

	wealthID := resolvedWealthID
	if strings.TrimSpace(wealthHint) != "" {
		wealthID, err = resolveUniqueWealthHint(ctx, tx, state.HouseholdID, wealthHint)
	} else if wealthID == "" {
		wealthID, err = resolveUniqueWealthHint(ctx, tx, state.HouseholdID, strings.TrimSpace(institution+" "+observationHint))
	}
	if err != nil || wealthID == "" {
		result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
		return result, true, nil
	}

	if strings.TrimSpace(amount) == "" {
		if err = tx.QueryRow(ctx, `SELECT observed_value_idr::text FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING' FOR UPDATE`, binding.TargetID, state.HouseholdID).Scan(&amount); err != nil {
			return result, true, err
		}
	}
	amountInt, ok := new(big.Int).SetString(amount, 10)
	if !ok || amountInt.Sign() <= 0 || amountInt.String() != amount {
		result.Status = "INVALID_AMOUNT"
		return result, true, nil
	}
	var compatible bool
	if err = tx.QueryRow(ctx, `SELECT transfer_wealth_compatible('ASSET_PURCHASE',$1::uuid,$2)`, wealthID, state.HouseholdID).Scan(&compatible); err != nil {
		return result, true, err
	}
	if !compatible {
		result.Status = "INCOMPATIBLE_WEALTH_ACCOUNT"
		return result, true, nil
	}

	localAt := at.In(jakartaLocation())
	dayStart := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), 0, 0, 0, 0, jakartaLocation()).UTC()
	dayEnd := dayStart.AddDate(0, 0, 1)
	type candidate struct {
		id, kind, status, purpose, existingWealth string
		at                                      time.Time
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text,type,status,COALESCE(purpose,''),COALESCE(related_wealth_account_id::text,''),transaction_at
		FROM transaction
		WHERE household_id=$1 AND account_id=$2 AND type IN ('TRANSFER','UNCLASSIFIED')
		  AND status<>'VOIDED' AND amount=$3 AND transaction_at >= $4 AND transaction_at < $5
		ORDER BY abs(extract(epoch FROM transaction_at-$6::timestamptz)),id
		LIMIT 2`, state.HouseholdID, accountID, amount, dayStart, dayEnd, at.UTC())
	if err != nil {
		return result, true, err
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.kind, &c.status, &c.purpose, &c.existingWealth, &c.at); err != nil {
			rows.Close()
			return result, true, err
		}
		candidates = append(candidates, c)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, true, err
	}
	rows.Close()
	if len(candidates) > 1 {
		result.Status = "TRANSFER_RECONCILIATION_REQUIRED"
		result.Facts = map[string]any{"candidate_count": len(candidates)}
		return result, true, nil
	}

	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}

	transactionID := ""
	if len(candidates) == 1 {
		candidate := candidates[0]
		if candidate.at.Sub(at).Abs() >= time.Minute {
			result.Status = "TRANSFER_RECONCILIATION_REQUIRED"
			result.Facts = map[string]any{"candidate_count": 1}
			return result, true, nil
		}
		transactionID = candidate.id

		// A confirmed transfer is authoritative finance state. Reuse it only when
		// it already has the exact ASSET_PURCHASE semantics requested here. Never
		// overwrite a savings/internal/investment transfer merely because amount
		// and time happen to match the Wealth observation.
		if candidate.kind == "TRANSFER" && candidate.status == "CONFIRMED" {
			if candidate.purpose != "ASSET_PURCHASE" || candidate.existingWealth != wealthID {
				result.Status = "TRANSFER_RECONCILIATION_REQUIRED"
				result.Facts = map[string]any{"candidate_count": 1}
				return result, true, nil
			}
		} else if candidate.kind == "UNCLASSIFIED" || candidate.status == "NEEDS_REVIEW" {
			updated, updateErr := tx.Exec(ctx, `
				UPDATE transaction
				SET type='TRANSFER',status='CONFIRMED',category_id=NULL,purpose='ASSET_PURCHASE',
				    related_wealth_account_id=$2::uuid,
				    description=COALESCE(NULLIF(description,''),'Pembelian investasi dari observasi Wealth'),
				    confirmed_at=COALESCE(confirmed_at,now()),voided_at=NULL,updated_at=now()
				WHERE id=$1 AND household_id=$3 AND status<>'VOIDED'`, transactionID, wealthID, state.HouseholdID)
			if updateErr != nil {
				return result, true, updateErr
			}
			if updated.RowsAffected() != 1 {
				result.Status = "TRANSFER_RECONCILIATION_REQUIRED"
				return result, true, nil
			}
			if _, err = tx.Exec(ctx, `UPDATE transaction_proposal SET proposed_type='TRANSFER',proposal_status='ACCEPTED',metadata_json=metadata_json||jsonb_build_object('purpose','ASSET_PURCHASE','agent_sprint',1),updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, transactionID); err != nil {
				return result, true, err
			}
		} else {
			result.Status = "TRANSFER_RECONCILIATION_REQUIRED"
			result.Facts = map[string]any{"candidate_count": 1}
			return result, true, nil
		}
	} else {
		if err = tx.QueryRow(ctx, `
			INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,created_by_user_id,purpose,related_wealth_account_id,confirmed_at)
			VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,'Pembelian investasi dari observasi Wealth',$5,'ASSET_PURCHASE',$6::uuid,now())
			RETURNING id`, state.HouseholdID, accountID, amount, at, userID, wealthID).Scan(&transactionID); err != nil {
			return result, true, err
		}
	}

	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',1,jsonb_build_object('review_request_id',$3::uuid,'agent_sprint',1)) ON CONFLICT DO NOTHING`, transactionID, state.SourceEventID, binding.ReviewRequestID); err != nil {
		return result, true, err
	}
	if originalSource != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_IMAGE',1,jsonb_build_object('reclassified_from','WEALTH_OBSERVATION','observation_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, originalSource, binding.TargetID); err != nil {
			return result, true, err
		}
	}

	updatedObservation, err := tx.Exec(ctx, `UPDATE wealth_observation SET status='DISMISSED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status='PENDING'`, binding.TargetID, state.HouseholdID)
	if err != nil {
		return result, true, err
	}
	if updatedObservation.RowsAffected() != 1 {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE document SET document_type='TRANSACTION_HISTORY_SCREENSHOT',status='EXTRACTED',updated_at=now() WHERE id=(SELECT document_id FROM wealth_observation WHERE id=$1)`, binding.TargetID); err != nil {
		return result, true, err
	}
	if err = resolveAgentBoundReviewRequestTx(ctx, tx, binding, userID, "RECLASSIFIED_ASSET_PURCHASE"); err != nil {
		return result, true, err
	}
	if originalSource != "" {
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, originalSource, state.HouseholdID); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, state.SourceEventID, state.HouseholdID); err != nil {
		return result, true, err
	}
	refs, err := persistAgentTransactionReferencesTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, fmt.Sprintf("p%dr0", state.ModelPhases), []string{transactionID})
	if err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RECLASSIFY_WEALTH_OBSERVATION','wealth_observation',$3,jsonb_build_object('transaction_id',$4::uuid,'purpose','ASSET_PURCHASE','agent_sprint',1))`, state.HouseholdID, userID, binding.TargetID, transactionID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}

	result.Status = "RESOLVED"
	result.References = refs
	result.Mutation = map[string]any{
		"action":         "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE",
		"amount_idr":     amount,
		"transaction_at": at.In(jakartaLocation()).Format(time.RFC3339),
	}
	return result, true, nil
}

// executeAgentSpecializedSideEffectStrict centralizes Sprint 1 safety
// overrides without changing deterministic callback behavior.
func (p *Processor) executeAgentSpecializedSideEffectStrict(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	if call.Name == "resolve_merchant_learning" {
		return p.agentResolveBoundMerchantLearningStrict(ctx, state, call, args)
	}
	if call.Name == "resolve_review" && state.ReviewBinding != nil {
		switch state.ReviewBinding.Kind {
		case "CYCLE_RESIDUAL":
			return p.agentResolveBoundResidualWithHints(ctx, state, call, args)
		case "WEALTH_OBSERVATION":
			action, _ := args["action"].(string)
			if action == "RECORD_ASSET_PURCHASE" {
				return p.agentResolveBoundWealthAssetPurchaseAtomic(ctx, state, call, args, state.ReviewBinding)
			}
		}
	}
	return p.executeAgentSpecializedSideEffectBound(ctx, state, call, args)
}
