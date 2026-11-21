package document

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/categoryctx"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

func categoryQuestionCriteria(categories []categoryOption) ([]string, map[string]any) {
	slugs := make([]string, 0, len(categories))
	choices := make([]judgment.CategoryChoice, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
		choices = append(choices, judgment.CategoryChoice{Slug: category.Slug, Label: category.Label})
	}
	return slugs, judgment.CategoryCriteria(choices)
}

const ReceiptCategoryPolicyVersion = "2026-10-receipt-category2"

var errReceiptCategoryProviderUnavailable = errors.New("receipt category judgment provider unavailable")

var receiptCategoryPolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}

type receiptCategoryProvenance struct {
	Asked          bool
	Model          string
	Choice         string
	Decided        bool
	ProviderFailed bool
}

func (p *Processor) receiptCategoryRescue(ctx context.Context, sourceEventID, householdID string, value receiptExtraction, at time.Time, categories []categoryOption) (string, receiptCategoryProvenance) {
	if p.verifier == nil || len(categories) < 2 {
		return "", receiptCategoryProvenance{}
	}
	slugs, criteria := categoryQuestionCriteria(categories)
	state := map[string]any{
		"user_text":  "<untrusted_receipt_merchant>" + strings.TrimSpace(value.Merchant) + "</untrusted_receipt_merchant>",
		"amount_idr": value.Total,
		"local_time": categoryctx.LocalTime(at),
	}
	history, err := categoryctx.History(ctx, p.pool, householdID, value.Merchant, slugs)
	if err != nil {
		return "", receiptCategoryProvenance{Asked: true, ProviderFailed: true}
	}
	if history != nil {
		state[categoryctx.StateKey] = history
	}
	if len(value.Items) > 0 {
		names := make([]string, 0, len(value.Items))
		for _, item := range value.Items {
			if name := strings.TrimSpace(item.Name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			state["line_items"] = "<untrusted_receipt_items>" + strings.Join(names, "; ") + "</untrusted_receipt_items>"
		}
	}
	ctx = judgment.WithPhaseMetadata(ctx, "RESIDUAL_CATEGORY", ReceiptCategoryPolicyVersion, []string{})
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-receipt-category", judgment.Request{State: state, Questions: map[string]judgment.Question{
		"category": {Type: "choice", Instructions: "Which single household category best describes this receipt expense? Answer with the closest supplied category slug, or OTHER_OR_UNCLEAR when no category is safe.", Criteria: criteria},
	}})
	if err != nil {
		return "", receiptCategoryProvenance{Asked: true, ProviderFailed: true}
	}
	provenance := receiptCategoryProvenance{Asked: true, Model: result.Model}
	answer, ok := result.Answers["category"]
	if !ok {
		return "", provenance
	}
	provenance.Choice = answer.Choice
	if !judgment.AcceptChoice(answer, criteria, receiptCategoryPolicy) {
		return "", provenance
	}
	for _, category := range categories {
		if category.Slug == answer.Choice {
			provenance.Decided = true
			return category.ID, provenance
		}
	}
	return "", provenance
}

func recordReceiptCategoryDecision(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, provenance receiptCategoryProvenance, outcome string) error {
	if !provenance.Asked {
		return nil
	}
	summary, err := json.Marshal(map[string]any{"choice": provenance.Choice, "decided": provenance.Decided})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO judgment_decision(household_id,source_event_id,task,model,policy_version,question_keys,answer_summary_json,outcome) VALUES($1,$2,'RECEIPT_CATEGORY',NULLIF($3,''),$4,ARRAY['category']::text[],$5::jsonb,$6)`, householdID, sourceEventID, provenance.Model, ReceiptCategoryPolicyVersion, string(summary), outcome)
	return err
}
