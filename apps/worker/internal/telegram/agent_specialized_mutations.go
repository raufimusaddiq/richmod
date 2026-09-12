package telegram

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func isAgentSpecializedSideEffect(name string) bool {
	switch name {
	case "record_transfer", "resolve_review", "resolve_salary_choice", "resolve_merchant_learning":
		return true
	default:
		return false
	}
}

// executeAgentSpecializedSideEffect keeps the Go-owned finance policy boundary
// while returning authoritative structured outcomes for natural LLM synthesis.
func (p *Processor) executeAgentSpecializedSideEffect(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	switch call.Name {
	case "record_transfer":
		return p.agentRecordTransfer(ctx, state, call, args)
	case "resolve_review":
		return p.agentResolveReview(ctx, state, call, args)
	case "resolve_salary_choice":
		return p.agentResolveSalaryChoice(ctx, state, call, args)
	case "resolve_merchant_learning":
		return p.agentResolveMerchantLearning(ctx, state, call, args)
	default:
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, true, fmt.Errorf("unsupported specialized side effect %q", call.Name)
	}
}

func (p *Processor) agentRecordTransfer(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	amount, _ := args["amount_idr"].(string)
	sourceHint, _ := args["source_account_hint"].(string)
	wealthHint, _ := args["destination_wealth_account_hint"].(string)
	purpose, _ := args["purpose"].(string)
	description, _ := args["description"].(string)

	value, ok := new(big.Int).SetString(amount, 10)
	if !ok || value.Sign() <= 0 || value.String() != amount {
		result.Status = "INVALID_AMOUNT"
		result.Facts = map[string]any{"missing_or_invalid": []string{"amount_idr"}}
		return result, true, nil
	}
	dateReference, _ := args["date_reference"].(string)
	explicitDate, _ := args["explicit_date"].(string)
	localTime, _ := args["local_time"].(string)
	at, err := resolveTime(state.Now, stringPtr(dateReference), stringPtr(explicitDate), stringPtr(localTime))
	if err != nil {
		result.Status = "INVALID_TIME"
		result.Facts = map[string]any{"missing_or_invalid": []string{"date_reference", "explicit_date", "local_time"}}
		return result, true, nil
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)

	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, fmt.Errorf("re-authorize Telegram identity: %w", err)
	}
	accountID, err := resolveUniqueAccountHint(ctx, tx, state.HouseholdID, sourceHint)
	if err != nil {
		result.Status = "ACCOUNT_AMBIGUOUS"
		result.Facts = map[string]any{"source_account_hint": clean(sourceHint, 160)}
		return result, true, nil
	}
	wealthID := ""
	if purpose != "INTERNAL_TRANSFER" {
		wealthID, err = resolveUniqueWealthHint(ctx, tx, state.HouseholdID, wealthHint)
		if err != nil {
			result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
			result.Facts = map[string]any{"wealth_account_hint": clean(wealthHint, 160)}
			return result, true, nil
		}
	}
	var compatible bool
	if err = tx.QueryRow(ctx, `SELECT transfer_wealth_compatible($1,NULLIF($2,'')::uuid,$3)`, purpose, wealthID, state.HouseholdID).Scan(&compatible); err != nil {
		return result, true, err
	}
	if !compatible {
		result.Status = "INCOMPATIBLE_TRANSFER_PURPOSE"
		result.Facts = map[string]any{"purpose": purpose}
		return result, true, nil
	}

	var existing string
	err = tx.QueryRow(ctx, `SELECT t.id FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1 AND t.household_id=$2 LIMIT 1`, state.SourceEventID, state.HouseholdID).Scan(&existing)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return result, true, err
		}
		result.Status = "NO_OP_DUPLICATE"
		result.Mutation = map[string]any{"action": "TRANSFER_ALREADY_RECORDED", "amount_idr": amount, "purpose": purpose}
		refs, refErr := p.persistAgentTransactionReferences(ctx, state.HouseholdID, state.SourceEventID, state.Update, fmt.Sprintf("p%dr0", state.ModelPhases), []string{existing})
		if refErr == nil {
			result.References = refs
		}
		return result, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, true, err
	}

	dayStart := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, jakartaLocation()).UTC()
	dayEnd := dayStart.AddDate(0, 0, 1)
	type candidate struct {
		id, kind, status, existingPurpose, existingWealth string
	}
	rows, err := tx.Query(ctx, `SELECT id::text,type,status,purpose,COALESCE(related_wealth_account_id::text,'') FROM transaction WHERE household_id=$1 AND account_id=$2 AND type IN ('TRANSFER','UNCLASSIFIED') AND status<>'VOIDED' AND amount=$3 AND transaction_at >= $4 AND transaction_at < $5 ORDER BY abs(extract(epoch FROM transaction_at-$6::timestamptz)),id LIMIT 11`, state.HouseholdID, accountID, amount, dayStart, dayEnd, at)
	if err != nil {
		return result, true, err
	}
	var candidates []candidate
	for rows.Next() {
		var candidate candidate
		if err = rows.Scan(&candidate.id, &candidate.kind, &candidate.status, &candidate.existingPurpose, &candidate.existingWealth); err != nil {
			rows.Close()
			return result, true, err
		}
		candidates = append(candidates, candidate)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, true, err
	}
	rows.Close()
	if len(candidates) > 10 {
		result.Status = "TOO_MANY_CANDIDATES"
		result.Facts = map[string]any{"candidate_count": len(candidates)}
		return result, true, nil
	}

	intent := transferReconciliationIntent{accountID: accountID, amount: amount, at: at, description: clean(description, 500), purpose: purpose, wealthID: wealthID}
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.id)
	}
	if len(candidates) > 1 {
		tx.Rollback(ctx)
		return p.agentStageTransferReview(ctx, state, call, intent, candidateIDs, "MULTIPLE_CANDIDATES")
	}

	transactionID := ""
	if len(candidates) == 1 {
		candidate := candidates[0]
		if strings.TrimSpace(localTime) == "" || candidate.kind != "TRANSFER" || candidate.status != "CONFIRMED" || candidate.existingPurpose != purpose || candidate.existingWealth != wealthID {
			tx.Rollback(ctx)
			return p.agentStageTransferReview(ctx, state, call, intent, candidateIDs, "POSSIBLE_DUPLICATE")
		}
		var exact bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction WHERE id=$1 AND transaction_at >= $2 AND transaction_at < $2 + interval '1 minute')`, candidate.id, at.UTC()).Scan(&exact); err != nil {
			return result, true, err
		}
		if !exact {
			tx.Rollback(ctx)
			return p.agentStageTransferReview(ctx, state, call, intent, candidateIDs, "TIME_MISMATCH")
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction SET purpose=$2,related_wealth_account_id=NULLIF($3,'')::uuid,description=COALESCE(NULLIF(description,''),NULLIF($4,'')),updated_at=now() WHERE id=$1`, candidate.id, purpose, wealthID, clean(description, 500)); err != nil {
			return result, true, err
		}
		transactionID = candidate.id
	} else {
		if err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,counterparty_name,created_by_user_id,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,NULLIF($5,''),NULL,$6,$7,NULLIF($8,'')::uuid,now()) RETURNING id`, state.HouseholdID, accountID, amount, at, clean(description, 500), userID, purpose, wealthID).Scan(&transactionID); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'TELEGRAM_TEXT',1) ON CONFLICT DO NOTHING`, transactionID, state.SourceEventID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}

	result.Status = "CONFIRMED"
	result.Mutation = map[string]any{"action": "TRANSFER_RECORDED", "amount_idr": amount, "purpose": purpose, "transaction_at": at.In(jakartaLocation()).Format(time.RFC3339)}
	refs, refErr := p.persistAgentTransactionReferences(ctx, state.HouseholdID, state.SourceEventID, state.Update, fmt.Sprintf("p%dr0", state.ModelPhases), []string{transactionID})
	if refErr == nil {
		result.References = refs
	}
	return result, true, nil
}

func (p *Processor) agentStageTransferReview(ctx context.Context, state *agentState, call gateway.ToolCall, intent transferReconciliationIntent, candidateIDs []string, reason string) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	if candidateIDs == nil {
		candidateIDs = []string{}
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, state.SourceEventID, state.HouseholdID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,proposed_wealth_account_id,candidate_transaction_ids) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,NULLIF($8,'')::uuid,$9::uuid[]) ON CONFLICT(source_event_id) WHERE financial_email_observation_id IS NULL DO UPDATE SET candidate_transaction_ids=EXCLUDED.candidate_transaction_ids,updated_at=now(),status='OPEN',resolved_at=NULL,resolved_by_user_id=NULL`, state.HouseholdID, state.SourceEventID, intent.accountID, intent.amount, intent.at, intent.description, intent.purpose, intent.wealthID, candidateIDs); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) SELECT $1,$2,'TRANSFER_CLASSIFICATION','OPEN' WHERE NOT EXISTS (SELECT 1 FROM review_item WHERE source_event_id=$2 AND status IN ('PENDING_SEND','OPEN'))`, state.HouseholdID, state.SourceEventID); err != nil {
		return result, true, err
	}
	var itemID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM review_item WHERE household_id=$1 AND source_event_id=$2 AND status IN ('PENDING_SEND','OPEN') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, state.HouseholdID, state.SourceEventID).Scan(&itemID); err != nil {
		return result, true, err
	}
	var reviewID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM review_request WHERE household_id=$1 AND review_item_id=$2 AND status IN ('PENDING_SEND','OPEN') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, state.HouseholdID, itemID).Scan(&reviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,review_type,telegram_chat_id,status) VALUES($1,$2,'TRANSFER_CLASSIFICATION',$3,'OPEN') RETURNING id`, state.HouseholdID, itemID, state.Update.Message.Chat.ID).Scan(&reviewID)
	}
	if err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2) ON CONFLICT(review_request_id,telegram_chat_id) DO NOTHING`, reviewID, state.Update.Message.Chat.ID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	candidateRefs := make([]string, len(candidateIDs))
	for index := range candidateIDs {
		candidateRefs[index] = fmt.Sprintf("candidate_%d", index+1)
	}
	result.Status = "NEEDS_REVIEW"
	result.Mutation = map[string]any{"action": "TRANSFER_RECONCILIATION_STAGED", "amount_idr": intent.amount, "purpose": intent.purpose}
	result.Review = map[string]any{"required": true, "review_type": "TRANSFER_RECONCILIATION", "reason": reason, "candidate_refs": candidateRefs}
	return result, true, nil
}

func (p *Processor) agentResolveSalaryChoice(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	choice, _ := args["choice"].(string)
	if choice != "PRIMARY" && choice != "ORDINARY" && choice != "IGNORE" {
		result.Status = "INVALID_CHOICE"
		return result, true, nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return result, true, err }
	defer tx.Rollback(ctx)
	var pendingID, transactionID, employer, period, payDate string
	err = tx.QueryRow(ctx, `SELECT id,transaction_id,employer,payroll_period::text,pay_date::text FROM salary_pending_choice WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, state.HouseholdID, state.Update.Message.From.ID, state.Update.Message.Chat.ID).Scan(&pendingID, &transactionID, &employer, &period, &payDate)
	if errors.Is(err, pgx.ErrNoRows) { result.Status = "NO_PENDING_SALARY_CHOICE"; return result, true, nil }
	if err != nil { return result, true, err }
	resolvedStatus := choice
	if choice == "IGNORE" {
		if _, err = tx.Exec(ctx, `UPDATE transaction SET status='VOIDED',updated_at=now() WHERE id=$1 AND household_id=$2`, transactionID, state.HouseholdID); err != nil { return result,true,err }
		resolvedStatus = "IGNORED"
		if _, err = tx.Exec(ctx, `UPDATE salary_pending_choice SET status='IGNORED',resolved_at=now() WHERE id=$1`, pendingID); err != nil { return result,true,err }
	} else {
		normalized := strings.ToLower(strings.Join(strings.Fields(employer), " "))
		if choice == "PRIMARY" {
			if _, err = tx.Exec(ctx, `UPDATE salary_source SET is_primary=false,updated_at=now() WHERE household_id=$1 AND active AND is_primary`, state.HouseholdID); err != nil { return result,true,err }
		}
		var salarySourceID string
		if err = tx.QueryRow(ctx, `INSERT INTO salary_source(household_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4) ON CONFLICT(household_id,normalized_employer) WHERE active DO UPDATE SET employer=excluded.employer,is_primary=excluded.is_primary,updated_at=now() RETURNING id`, state.HouseholdID, employer, normalized, choice == "PRIMARY").Scan(&salarySourceID); err != nil { return result,true,err }
		var salaryEventID string
		if err = tx.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) SELECT $1,$2,$3::date,$4::date,t.amount,'IDR',t.id,'CONFIRMED',$5 FROM transaction t WHERE t.id=$6 AND t.household_id=$2 ON CONFLICT (salary_source_id,payroll_period) DO UPDATE SET transaction_id=EXCLUDED.transaction_id,pay_date=EXCLUDED.pay_date,net_pay=EXCLUDED.net_pay,source_event_id=EXCLUDED.source_event_id,status='CONFIRMED' RETURNING id`, salarySourceID, state.HouseholdID, period, payDate, state.SourceEventID, transactionID).Scan(&salaryEventID); err != nil { return result,true,err }
		if choice == "PRIMARY" {
			if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('GENERATE_CYCLE_RESIDUAL_REVIEW',jsonb_build_object('household_id',$1::uuid,'end_salary_event_id',$2::uuid),5) ON CONFLICT DO NOTHING`, state.HouseholdID, salaryEventID); err != nil { return result,true,err }
		}
		if _, err = tx.Exec(ctx, `UPDATE salary_pending_choice SET status=$2,resolved_at=now() WHERE id=$1`, pendingID, choice); err != nil { return result,true,err }
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil { return result,true,err }
	if err = tx.Commit(ctx); err != nil { return result,true,err }
	result.Status = resolvedStatus
	result.Mutation = map[string]any{"action": "SALARY_CHOICE_RESOLVED", "choice": choice, "employer": employer, "payroll_period": period, "pay_date": payDate}
	return result, true, nil
}

func (p *Processor) agentResolveMerchantLearning(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	remember, _ := args["remember"].(bool)
	var reviewID, transactionID string
	err := p.pool.QueryRow(ctx, `SELECT r.id,r.transaction_id FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND r.status='OPEN' AND c.state='AWAITING_CONFIRMATION' AND t.status='CONFIRMED' AND rr.telegram_chat_id=$2 ORDER BY r.created_at DESC LIMIT 1`, state.HouseholdID, state.Update.Message.Chat.ID).Scan(&reviewID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) { result.Status = "NO_MERCHANT_LEARNING_PENDING"; return result,true,nil }
	if err != nil { return result,true,err }
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{}); if err != nil { return result,true,err }; defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil { return result,true,err }
	var merchantName, categoryName string
	if remember {
		var merchantID, categoryID string
		if err = tx.QueryRow(ctx, `SELECT t.merchant_id,t.category_id,m.normalized_name,c.name FROM transaction t JOIN merchant m ON m.id=t.merchant_id JOIN category c ON c.id=t.category_id WHERE t.id=$1 AND t.household_id=$2 AND t.status='CONFIRMED' AND t.merchant_id IS NOT NULL AND t.category_id IS NOT NULL`, transactionID, state.HouseholdID).Scan(&merchantID, &categoryID, &merchantName, &categoryName); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 AND household_id=$1 ON CONFLICT(household_id,raw_name) DO UPDATE SET default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, state.HouseholdID, merchantID, categoryID); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REMEMBER_MERCHANT','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'merchant_id',$5::uuid,'category_id',$6::uuid,'agent_sprint',1))`, state.HouseholdID, userID, transactionID, reviewID, merchantID, categoryID); err != nil { return result,true,err }
	} else {
		_ = tx.QueryRow(ctx, `SELECT COALESCE(m.normalized_name,''),COALESCE(c.name,'') FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id LEFT JOIN category c ON c.id=t.category_id WHERE t.id=$1 AND t.household_id=$2`, transactionID, state.HouseholdID).Scan(&merchantName, &categoryName)
	}
	if _, err = tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND status='OPEN'`, reviewID); err != nil { return result,true,err }
	if err = resolveCanonicalReviewItem(ctx, tx, reviewID, "TELEGRAM_MERCHANT_DECISION"); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($3,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$1::uuid,'remember_merchant',$4::boolean)) ON CONFLICT DO NOTHING`, reviewID, state.SourceEventID, transactionID, remember); err != nil { return result,true,err }
	if err = tx.Commit(ctx); err != nil { return result,true,err }
	result.Status = "CONFIRMED"
	result.Mutation = map[string]any{"action": "MERCHANT_LEARNING_RESOLVED", "remember": remember, "merchant": merchantName, "category": categoryName}
	return result, true, nil
}
