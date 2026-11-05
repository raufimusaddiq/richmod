package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
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
	case "pending_batch_decision":
		return p.agentPendingBatchDecision(ctx, state, call, args)
	case "dispute_evidence_link":
		return p.agentDisputeEvidenceLink(ctx, state, call)
	default:
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, false, fmt.Errorf("unsupported core side-effect tool %q", call.Name)
	}
}

func (p *Processor) agentRecordTransaction(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, metadata gateway.Metadata) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	value, err := nativeValidatedExtraction(args, state.Now)
	if err != nil {
		// validateCreate already checks every field; anything left is still the
		// model's to correct, never a job error.
		return result, true, &agentRecoverable{Status: "INVALID_TRANSACTION_FIELDS", Reason: err.Error(),
			NextStep: "correct the fields and call record_transaction again, or ask the user for the missing detail",
			UserCopy: "Detail transaksinya belum lengkap, jadi belum ada yang dicatat. Sebutkan nominal dan tanggalnya, ya."}
	}
	// The user just sent evidence that already produced this transaction: a second
	// ledger row for the same real event is refused here, whatever the model asked.
	if p.evidenceAlreadyRecorded(ctx, state.HouseholdID, state.FreshEvidenceDocuments, value.Type, value.Amount, value.Merchant) {
		result.Status = "ALREADY_RECORDED_FROM_EVIDENCE"
		result.Facts = map[string]any{"hint": "the document you just sent is already recorded; correct that transaction instead, or ask the user for what differs"}
		return result, true, nil
	}
	if value.Type == "EXPENSE" {
		token, err := p.offerExistingEditForTurn(ctx, state, value, &result)
		if err != nil {
			return result, true, err
		}
		if token != "" {
			return result, false, nil
		}
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

	// The typed merchant gets the household's merchant identity, as a review
	// answer does, so a confirmed merchant rule applies here and the
	// merchant-learning question can follow a later review.
	merchantID, remembered, err := resolveEntryMerchantTx(ctx, tx, state.HouseholdID, value.Merchant)
	if err != nil {
		return result, true, err
	}
	if value.CategorySlug == "" && remembered != nil && value.Type == "EXPENSE" {
		value.CategorySlug = remembered.Slug
	}
	var categoryID *string
	if value.CategorySlug != "" {
		var id string
		err = tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, state.HouseholdID, value.CategorySlug).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, true, &agentRecoverable{Status: "INVALID_CATEGORY", Reason: fmt.Sprintf("%q is not one of allowed_category_slugs", value.CategorySlug),
				NextStep: "call record_transaction again with a slug from allowed_category_slugs, or with category_slug null so the user picks it on the review card",
				UserCopy: "Kategori itu tidak ada di daftar keluarga ini, jadi belum ada yang dicatat. Sebutkan kategori yang tersedia, ya."}
		}
		if err != nil {
			return result, true, fmt.Errorf("validate category: %w", err)
		}
		categoryID = &id
	}

	// The conversational record_transaction tool is another canonical mutation
	// boundary, so it consumes the same semantic decision object as every other
	// path instead of grading the generative extraction itself (ADR-038).
	// A category query failure is infrastructure state, not an empty household
	// category set: retry the job instead of downgrading a valid record to review.
	allowedCategories, err := p.categorySlugs(ctx, state.HouseholdID)
	if err != nil {
		return result, true, err
	}
	exactCategory := remembered != nil && remembered.Slug == value.CategorySlug
	if value.CategorySlug != "" && !exactCategory {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_alias ma JOIN category c ON c.id=ma.default_category_id WHERE ma.household_id=$1 AND ma.auto_apply AND ma.created_from_user_confirmation AND c.slug=$2)`, state.HouseholdID, value.CategorySlug).Scan(&exactCategory); err != nil {
			return result, true, err
		}
	}
	decision, decisionErr := p.semanticDecisionForRecord(ctx, state, value, allowedCategories, exactCategory)
	if decisionErr != nil {
		return result, true, decisionErr
	}
	if decision.CategoryAccepted {
		value.CategorySlug = decision.CategorySlug
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, state.HouseholdID, decision.CategorySlug).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				result.Status = "INVALID_CATEGORY"
				result.Facts = map[string]any{"category_slug": decision.CategorySlug}
				return result, true, nil
			}
			return result, true, fmt.Errorf("resolve decided category: %w", err)
		}
		categoryID = &id
	}
	if !decision.DateSupported {
		state.ResidualDimensions = appendIfMissing(state.ResidualDimensions, "transaction_at")
	}
	autoConfirm := decision.decisionAllowed() && (value.Type == "INCOME" || categoryID != nil)
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
		"decision_source":     decision.DecisionSource,
		"policy_version":      decision.PolicyVersion,
		"residual_dimensions": state.ResidualDimensions,
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
		INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,note,counterparty_name,source_confidence,classification_confidence,created_by_user_id,confirmed_at,auto_confirmed_at,merchant_id)
		VALUES($1,$2,$3,$4,'IDR',$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12,CASE WHEN $3='CONFIRMED' THEN now() END,CASE WHEN $13 THEN now() END,NULLIF($14::text,'')::uuid)
		RETURNING id`, state.HouseholdID, value.Type, transactionStatus, value.Amount, value.TransactionAt, categoryID, value.Description, value.Note, value.Merchant, value.Confidence, value.CategoryConfidence, userID, autoConfirm, merchantID).Scan(&transactionID); err != nil {
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
	if autoConfirm {
		if err = reviewdomain.RefreshOpenCycleResiduals(ctx, tx, state.HouseholdID, value.TransactionAt, userID); err != nil {
			return result, true, err
		}
	}
	if !autoConfirm {
		reviewType := "AMBIGUOUS_CATEGORY"
		if contains(state.ResidualDimensions, "transaction_at") && contains(state.ResidualDimensions, "category") {
			reviewType = "TRANSACTION_FACTS_MISSING"
		} else if contains(state.ResidualDimensions, "transaction_at") {
			reviewType = "MISSING_TRANSACTION_DATE"
		} else if value.Merchant == "" {
			reviewType = "UNKNOWN_MERCHANT"
		}
		if err = EnqueueReviewRequest(ctx, tx, transactionID, reviewType, state.Update.Message.Chat.ID, state.Update.Message.MessageID, ReviewQuestion(value.Amount, value.Merchant)); err != nil {
			return result, true, err
		}
	}
	categoryName := ""
	if categoryID != nil {
		if err = tx.QueryRow(ctx, `SELECT name FROM category WHERE id=$1`, *categoryID).Scan(&categoryName); err != nil {
			return result, true, err
		}
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
		"category_name":  categoryName,
		"description":    value.Description,
		"transaction_at": value.TransactionAt.In(jakartaLocation()).Format(time.RFC3339),
		"time_precision": value.TimePrecision,
		"status":         transactionStatus,
	}
	if !autoConfirm {
		// The review card goes to this same chat, so the reply points at it
		// instead of sending the household to the Inbox.
		result.Review = map[string]any{"required": true, "reason": "TRANSACTION_NEEDS_REVIEW", "card_in_chat": true}
	}
	// The result is fully deterministic: the reply is queued with the mutation,
	// so a later failure cannot leave the household without an answer.
	if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	return result, false, nil
}
