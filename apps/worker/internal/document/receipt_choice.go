package document

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// ReceiptCategoryPolicyVersion marks the bar behind a receipt category rescue, so
// a stored decision stays reproducible (PRD §21). It reuses the same decisive
// answer policy as the screenshot row rescue and the Telegram category rescue.
const ReceiptCategoryPolicyVersion = "2026-09-receipt-category1"

var receiptCategoryPolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}

type receiptCategoryProvenance struct {
	Asked          bool
	Model          string
	Choice         string
	Decided        bool
	ProviderFailed bool
}

// receiptCategoryRescue asks the bounded plane one category question for a
// receipt whose only unresolved fact is the category. It returns the matched
// category ID only for a decisive, well-separated answer; anything undecided or
// a provider failure returns nil so the receipt keeps its category-only review
// (PRD §9, BDR-001 IR-05). A missing date is never a rescue input.
func (p *Processor) receiptCategoryRescue(ctx context.Context, sourceEventID string, value receiptExtraction, categories []categoryOption) (string, receiptCategoryProvenance) {
	if p.verifier == nil || len(categories) < 2 {
		return "", receiptCategoryProvenance{}
	}
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
	}
	criteria := judgment.CategoryCriteria(slugs)
	state := map[string]any{
		"user_text":  "<untrusted_receipt_merchant>" + strings.TrimSpace(value.Merchant) + "</untrusted_receipt_merchant>",
		"amount_idr": value.Total,
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
	ctx = judgment.WithPhaseMetadata(ctx, "RESIDUAL_CATEGORY", ReceiptCategoryPolicyVersion)
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
