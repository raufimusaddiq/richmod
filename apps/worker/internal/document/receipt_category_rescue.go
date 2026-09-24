package document

import (
	"context"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// resolveReceiptCategory is a rescue lane, not a mandatory second opinion.
// Vision extraction already had the first chance to return one constrained
// household category. Jev runs only when that category is still unresolved,
// so a clear receipt never pays an LLM -> Jev double-call tax.
func (p *Processor) resolveReceiptCategory(ctx context.Context, sourceEventID string, value receiptExtraction, categories []categoryOption) (string, error) {
	if p.verifier == nil || len(categories) < 2 {
		return "", nil
	}
	bySlug := make(map[string]string, len(categories))
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		bySlug[category.Slug] = category.ID
		slugs = append(slugs, category.Slug)
	}
	criteria := judgment.CategoryCriteria(slugs)
	itemNames := make([]string, 0, len(value.Items))
	for _, item := range value.Items {
		if name := strings.TrimSpace(item.Name); name != "" {
			itemNames = append(itemNames, name)
			if len(itemNames) >= 12 {
				break
			}
		}
	}
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-receipt-category-rescue", judgment.Request{
		State: map[string]any{
			"merchant":   "<untrusted_receipt_merchant>" + strings.TrimSpace(value.Merchant) + "</untrusted_receipt_merchant>",
			"items":      itemNames,
			"total_idr":  value.Total,
			"payment_hint": strings.TrimSpace(value.PaymentMethodHint),
		},
		Questions: map[string]judgment.Question{
			"category": {
				Type:         "choice",
				Instructions: "Choose the single household category that best describes this receipt. Use OTHER_OR_UNCLEAR when no category is safe.",
				Criteria:     criteria,
			},
		},
	})
	if err != nil {
		// This is an optional rescue after generative extraction. Provider failure
		// must not turn an otherwise reviewable receipt into a failed job.
		return "", nil
	}
	answer, ok := result.Answers["category"]
	if !ok || !judgment.AcceptChoice(answer, criteria, rowCategoryPolicy) {
		return "", nil
	}
	return bySlug[answer.Choice], nil
}
