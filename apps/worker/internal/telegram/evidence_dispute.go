package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/merchantmemory"
)

func evidenceDisputeTool() gateway.ToolDefinition {
	return gateway.ToolDefinition{Name: "dispute_evidence_link", Description: "Stage a review ONLY when the user explicitly says the exactly replied-to MERGED screenshot is a different/new transaction, not the already linked event. No IDs or amounts accepted. Go validates the current single-row link. Nothing is confirmed until a human presses the review confirmation button.", Parameters: objectSchema(map[string]any{}, []string{})}
}

type disputeCategory struct {
	id         string
	confidence *float64
}

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
		certain := 1.0
		return disputeCategory{id: memory.CategoryID, confidence: &certain}, nil
	case bySlug != "":
		return disputeCategory{id: bySlug, confidence: confidence}, nil
	case candidate != nil && *candidate != "":
		id, err := active("id::text", *candidate)
		return disputeCategory{id: id, confidence: confidence}, err
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
	staged, err := reviewdomain.StageEvidenceDispute(ctx, tx, reviewdomain.EvidenceDisputeCommand{HouseholdID: state.HouseholdID, ActorUserID: user, DocumentID: string(document), SourceEventID: state.SourceEventID, CategoryID: category.id, CategoryConfidence: category.confidence})
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
		facts, err := transactionReviewSummary(ctx, tx, state.HouseholdID, staged.TransactionID)
		if err != nil {
			return result, false, err
		}
		if err = EnqueueReviewRequest(ctx, tx, staged.TransactionID, "POSSIBLE_DUPLICATE", state.Update.Message.Chat.ID, state.Update.Message.MessageID, facts, decision); err != nil {
			return result, false, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-evidence-dispute',parser_version='1' WHERE id=$1::uuid`, state.SourceEventID); err != nil {
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
