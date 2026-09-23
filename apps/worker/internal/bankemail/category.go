package bankemail

import (
	"context"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// categoryDecisionPolicy versions the thresholds that turn a bounded category
// answer into an auto-confirm. It is separate from the evidence-verification
// policy so tuning one never silently moves the other.
const categoryDecisionPolicyVersion = "2026-09-bank-category1"

// categoryChoicePolicy requires a confident, well-separated top category. A
// coin-flip or a thin margin is exactly the EVIDENCE_GAP case the PRD wants a
// one-question review for, not a guess.
var categoryChoicePolicy = judgment.ChoicePolicy{MinTop: 0.70, MinMargin: 0.35, MinConfidence: 0.60}

// classifyExpenseCategory asks the bounded plane to pick one household category
// for an expense whose merchant was not learned. It returns the chosen category
// ID only when the answer is decisive; otherwise the caller opens a category-
// only review. The candidate set is server-owned (the household's active
// categories), so the model never sees or chooses a hidden canonical ID.
func (p *Processor) classifyExpenseCategory(ctx context.Context, household, sourceEventID string, extraction Extraction) (string, bool, error) {
	if p.verifier == nil {
		return "", false, nil
	}
	candidates, err := p.categoryCandidates(ctx, household)
	if err != nil || len(candidates) < 2 {
		// Fewer than two choices is not a bounded decision; let the ordinary
		// review path handle it.
		return "", false, err
	}
	descriptions := make(map[string]string, len(candidates))
	for slug, name := range candidates {
		descriptions[slug] = "Kategori pengeluaran " + name
	}
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-category", judgment.Request{
		State: map[string]any{
			"merchant":    strings.TrimSpace(value(extraction.Merchant)),
			"description": strings.TrimSpace(value(extraction.Description)),
			"amount_idr":  value(extraction.AmountIDR),
			"direction":   pointerValue(extraction.Direction),
			"categories":  candidateNames(candidates),
		},
		Questions: map[string]judgment.Question{
			"category": {Type: "choice", Instructions: "Which single category best describes this expense? Choose the closest household category; do not invent a new one.", Criteria: judgment.ChoiceCriteria(descriptions)},
		},
	})
	if err != nil {
		return "", false, err
	}
	answer, ok := result.Answers["category"]
	if !ok || !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(descriptions), categoryChoicePolicy) {
		return "", false, nil
	}
	categoryID, ok := candidates[answer.Choice]
	if !ok {
		return "", false, nil
	}
	return categoryID, true, nil
}

// categoryCandidates returns active household categories keyed by slug. A slug
// is the bounded value the model answers with; Go maps it back to the canonical
// category ID (PRD §7.5).
func (p *Processor) categoryCandidates(ctx context.Context, household string) (map[string]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT slug, name FROM category WHERE household_id=$1 AND active ORDER BY sort_order, name`, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := map[string]string{}
	for rows.Next() {
		var slug, name string
		if err := rows.Scan(&slug, &name); err != nil {
			return nil, err
		}
		candidates[slug] = name
	}
	return candidates, rows.Err()
}

func candidateNames(candidates map[string]string) []string {
	names := make([]string, 0, len(candidates))
	for slug := range candidates {
		names = append(names, slug)
	}
	return names
}
