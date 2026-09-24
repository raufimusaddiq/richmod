package financialemail

import (
	"context"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// ReconciliationPolicyVersion marks the thresholds behind the same-event ruling
// so a stored decision stays reproducible (PRD §18).
const ReconciliationPolicyVersion = "2026-09-jev2"

// sameEventAnswer is the bounded ruling over one already-narrowed candidate set:
// does the provider email describe the same real event as the ledger row Go
// already found? Go owns the narrowing (household, account, amount, direction,
// 24h window); Jev only rules on the survivors, and never searches the ledger or
// sees a hidden ID (PRD §20).
type sameEventAnswer struct {
	SameEvent bool
	Model     string
}

// reconcileSemantically asks the bounded plane whether the single remaining
// candidate is the same real event. Only called with exactly one candidate: with
// none there is nothing to reconcile, and with many the ambiguity is real and
// belongs in Review rather than in a forced choice.
//
// The candidate's canonical UUID is never sent. Jev rules on the semantics;
// Go keeps the mapping back to the row.
func (p *Processor) reconcileSemantically(ctx context.Context, requestID, amount string, at string, description string, candidateType, candidatePurpose, candidateAt string) (sameEventAnswer, error) {
	if p.verifier == nil {
		return sameEventAnswer{}, nil
	}
	ctx = judgment.WithPhaseMetadata(ctx, "OTHER_BOUNDED", ReconciliationPolicyVersion)
	result, err := p.verifier.Evaluate(ctx, requestID, judgment.Request{
		State: map[string]any{
			"provider_amount_idr":  amount,
			"provider_at":          at,
			"provider_description": description,
			"ledger_type":          candidateType,
			"ledger_purpose":       candidatePurpose,
			"ledger_at":            candidateAt,
		},
		Questions: map[string]judgment.Question{
			"same_real_event": {
				Type:         "noul",
				Instructions: "Decide whether the provider notification and the ledger entry describe the same real financial event. Answer only from the supplied facts.",
			},
		},
	})
	if err != nil {
		return sameEventAnswer{}, err
	}
	answer, ok := result.Answers["same_real_event"]
	if !ok {
		return sameEventAnswer{}, nil
	}
	// A Noul is only a yes when it is affirmatively high. An undecided middle band
	// leaves the candidate in Review, which is the safe default: reusing the wrong
	// row silently merges two real events.
	sameEvent, decided := judgment.AcceptNoul(answer, judgment.NoulPolicy{High: 0.85, Low: 0.15})
	return sameEventAnswer{SameEvent: sameEvent && decided, Model: result.Model}, nil
}

// canReconcileSemantically reports whether the candidate set is exactly the
// shape §20 permits a bounded ruling on: one survivor, nothing already reused.
func (p cashPlan) canReconcileSemantically() bool {
	return p.review == "TRANSFER_RECONCILIATION" && len(p.candidates) == 1 && p.existing == ""
}
