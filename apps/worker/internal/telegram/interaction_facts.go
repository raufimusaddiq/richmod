package telegram

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

const (
	purposeAsk  = "Ini untuk apa?"
	purposeHint = "Ketik tujuan transaksinya, singkat saja."
)

func factsReviewType(reviewType string) bool {
	switch reviewType {
	case "MISSING_TRANSACTION_DATE", "TRANSACTION_FACTS_MISSING", "UNKNOWN_PURPOSE":
		return true
	}
	return false
}

func purposeStepFrom(base Step, actions []string) Step {
	base.Kind, base.Fact, base.State, base.Back = stepFact, factPurpose, "AWAITING_PURPOSE", false
	base.Ask, base.Hint = purposeAsk, purposeHint
	base.Rows = nil
	if contains(actions, "IGNORE") {
		base.Rows = [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: callbackIgnore}}}
	}
	return base
}

func usablePurposeText(text string) (string, bool) {
	value := strings.Join(strings.Fields(text), " ")
	length := len([]rune(value))
	if length < 2 || length > 120 || strings.Contains(value, "?") || len(strings.Fields(value)) > 12 {
		return "", false
	}
	hasLetter := false
	for _, r := range value {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	normalized := normalizeAnswer(value)
	if !hasLetter || isYesNoAnswer(normalized) || isHelpCommand(normalized) || strings.HasPrefix(normalized, "/") || mentionsFinanceRequest(normalized) {
		return "", false
	}
	return clean(value, 160), true
}

func (p *Processor) recordFactAnswer(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, field, value string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'field',$4::text,'value',$5::text)) ON CONFLICT DO NOTHING`, review.TransactionID, sourceEventID, review.RequestID, field, value); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'UPDATE_REVIEW_DETAIL','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'field',$5::text,'value',$6::text))`, householdID, review.UserID, review.TransactionID, review.RequestID, field, value)
	return err
}

func (p *Processor) finishTransactionDate(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, at time.Time) (stepOutcome, error) {
	if review.TransactionID == "" {
		return staleTap(step), nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	remaining, err := reviewdomain.ApplyTransactionReviewFacts(ctx, sp, reviewdomain.ReviewFacts{HouseholdID: householdID, TransactionID: review.TransactionID, TransactionAt: &at})
	var missing *reviewdomain.ErrMissingFacts
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	case errors.As(err, &missing):
		_ = sp.Rollback(ctx)
		return reask(step, unusableAnswerAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	label := reviewdomain.FormatIndonesianDate(at.In(jakartaLocation()))
	if err := p.recordFactAnswer(ctx, sp, householdID, sourceEventID, review, "transaction_at", label); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if len(remaining) > 0 {
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		next, err := p.reloadedStep(ctx, tx, householdID, review)
		if err != nil {
			return stepOutcome{}, err
		}
		next.Step = next.Step.WithAck("✅ Tanggal " + label + " disimpan.")
		return next, nil
	}
	_, err = reviewdomain.ConfirmTransactionReview(ctx, sp, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID,
		Action: "TELEGRAM_DATE_SET", TransactionAt: &at, ResolveReview: true,
	})
	if errors.As(err, &missing) || errors.Is(err, reviewdomain.ErrMissingCategory) || errors.Is(err, reviewdomain.ErrAlreadyResolved) {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if reply, problem := merchantLinkProblemReply(err); problem {
		_ = sp.Rollback(ctx)
		return reask(step, reply), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, sp, householdID, review.TransactionID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return p.confirmedDone(ctx, tx, householdID, review, "Tanggal: "+label)
}

func (p *Processor) confirmedDone(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, note string) (stepOutcome, error) {
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	amount, err := transactionAmount(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	if review.TxType == "TRANSFER" {
		return stepOutcome{Step: transferDoneStep("Transfer", "", amount), Settled: true, Changed: true}, nil
	}
	return stepOutcome{Step: doneStep(outcome.Merchant, outcome.Category, amount, note), Settled: true, Changed: true}, nil
}

func (p *Processor) applyPurposeText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, text string) (stepOutcome, error) {
	value, ok := usablePurposeText(text)
	if !ok {
		return reask(step, unusableAnswerAck), nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	tag, err := sp.Exec(ctx, `UPDATE transaction SET description=$2,updated_at=now() WHERE id=$1 AND household_id=$3 AND status='NEEDS_REVIEW'`, review.TransactionID, value, householdID)
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if tag.RowsAffected() != 1 {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if _, err := sp.Exec(ctx, `UPDATE transaction_proposal SET description=$2,updated_at=now() WHERE household_id=$3 AND id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, review.TransactionID, value, householdID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := p.recordFactAnswer(ctx, sp, householdID, sourceEventID, review, "description", value); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{missingFacts}',COALESCE(decision->'missingFacts','[]'::jsonb)-'transaction_semantics'),updated_at=now() WHERE id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND')`, review.ItemID, householdID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	review.Decision = withoutMissingFact(review.Decision, "transaction_semantics")
	var categorized bool
	if err := sp.QueryRow(ctx, `SELECT type<>'EXPENSE' OR category_id IS NOT NULL FROM transaction WHERE id=$1 AND household_id=$2`, review.TransactionID, householdID).Scan(&categorized); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if !categorized || len(review.Decision.MissingFacts) > 0 {
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		next, ok, err := p.nextAfterFact(ctx, tx, householdID, review, "✅ Tujuannya disimpan.")
		if err != nil || !ok {
			return stepOutcome{}, err
		}
		return stepOutcome{Step: next, Changed: true}, nil
	}
	_, err = reviewdomain.ConfirmTransactionReview(ctx, sp, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID, Action: "TELEGRAM_CONFIRMED", Description: value, ResolveReview: true,
	})
	var missing *reviewdomain.ErrMissingFacts
	if errors.As(err, &missing) || errors.Is(err, reviewdomain.ErrMissingCategory) || errors.Is(err, reviewdomain.ErrAlreadyResolved) {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if reply, problem := merchantLinkProblemReply(err); problem {
		_ = sp.Rollback(ctx)
		return reask(step, reply), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, sp, householdID, review.TransactionID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return p.confirmedDone(ctx, tx, householdID, review, "")
}
