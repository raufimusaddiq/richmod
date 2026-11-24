package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) duplicateCandidatesForTransaction(ctx context.Context, householdID, transactionID string) ([]duplicateCandidate, error) {
	if transactionID == "" {
		return nil, nil
	}
	var sourceType, currency, amount string
	var sourceAt time.Time
	err := p.pool.QueryRow(ctx, `SELECT type::text,currency,amount::text,transaction_at FROM transaction WHERE id=$1 AND household_id=$2`, transactionID, householdID).
		Scan(&sourceType, &currency, &amount, &sourceAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load duplicate source: %w", err)
	}
	return duplicateCandidateRows(ctx, p.pool, householdID, transactionID, sourceType, currency, amount, sourceAt)
}

func (p *Processor) agentMergeDuplicateReview(ctx context.Context, state *agentState, call gateway.ToolCall, review agentTransactionReview, candidateRef string) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	if review.reviewType != "POSSIBLE_DUPLICATE" {
		result.Status = "UNSUPPORTED_REVIEW_ACTION"
		return result, true, nil
	}
	targetID, err := p.resolveTransactionReference(ctx, state.HouseholdID, state.Update, candidateRef)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "INVALID_CANDIDATE"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	candidates, err := p.duplicateCandidatesForTransaction(ctx, state.HouseholdID, review.transactionID)
	if err != nil {
		return result, true, err
	}
	isCandidate := false
	for _, candidate := range candidates {
		isCandidate = isCandidate || candidate.ID == targetID
	}
	if !isCandidate {
		result.Status = "INVALID_CANDIDATE"
		return result, true, nil
	}
	var userID string
	if err := p.pool.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		return result, true, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	_, err = reviewdomain.MergeDuplicateReview(ctx, tx, reviewdomain.DuplicateCommand{
		HouseholdID: state.HouseholdID, ActorUserID: userID, TransactionID: review.transactionID, TargetTransactionID: targetID,
		ReviewItemID: pendingReviewItemID(ctx, tx, review.reviewID), RequestID: review.reviewID, Action: "TELEGRAM_MERGE_REVIEW",
	})
	if errors.Is(err, reviewdomain.ErrDuplicateTargetInvalid) {
		result.Status = "INVALID_CANDIDATE"
		return result, true, nil
	}
	if errors.Is(err, reviewdomain.ErrAlreadyMerged) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID); err != nil {
		return result, true, err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, state.SourceEventID, state.HouseholdID); err != nil {
		return result, true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "DUPLICATE_MERGED"}
	return result, true, nil
}
