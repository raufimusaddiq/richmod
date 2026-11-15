package financialemail

import (
	"context"
	"strconv"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// ReconciliationPolicyVersion marks the thresholds behind the same-event ruling so stored decisions stay reproducible.
const ReconciliationPolicyVersion = "2026-09-jev2"

// sameEventAnswer is the bounded ruling on whether the email matches the ledger row Go found.
// Go owns narrowing (household, account, amount, direction, 24h window); Jev never sees a hidden ID.
type sameEventAnswer struct {
	SameEvent bool
	Model     string
}

// candidateChoice picks among deterministic survivors by anonymous index, keeping the index-to-row mapping
// private; a decisive CANDIDATE_n reuses that row, anything else keeps the review.
type candidateChoice struct {
	Index int // 1-based; 0 means no decisive pick
	Model string
}

// reconcileCandidates is used only when narrowing leaves several survivors; several candidates are not
// automatically human ambiguity.
func (p *Processor) reconcileCandidates(ctx context.Context, requestID, amount, at, description string, candidateFacts []string) (candidateChoice, error) {
	if p.verifier == nil || len(candidateFacts) == 0 || len(candidateFacts) > 10 {
		return candidateChoice{}, nil
	}
	criteria := map[string]string{"NONE_OR_UNCLEAR": "no candidate is safely the same real event"}
	state := map[string]any{
		"provider_amount_idr":  amount,
		"provider_at":          at,
		"provider_description": description,
	}
	for index, facts := range candidateFacts {
		key := "CANDIDATE_" + strconv.Itoa(index+1)
		criteria[key] = "ledger entry " + strconv.Itoa(index+1)
		state["candidate_"+strconv.Itoa(index+1)] = facts
	}
	ctx = judgment.WithPhaseMetadata(ctx, "OTHER_BOUNDED", ReconciliationPolicyVersion, []string{})
	result, err := p.verifier.Evaluate(ctx, requestID, judgment.Request{
		State: state,
		Questions: map[string]judgment.Question{
			"same_real_event": {Type: "choice", Instructions: "Choose the single ledger candidate that is the same real financial event as the provider notification, or NONE_OR_UNCLEAR. Answer only from the supplied facts.", Criteria: judgment.ChoiceCriteria(criteria)},
		},
	})
	if err != nil {
		return candidateChoice{}, err
	}
	answer, ok := result.Answers["same_real_event"]
	if !ok || answer.Choice == "NONE_OR_UNCLEAR" || !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(criteria), judgmentPolicyForCandidates) {
		return candidateChoice{Model: result.Model}, nil
	}
	index, err := strconv.Atoi(strings.TrimPrefix(answer.Choice, "CANDIDATE_"))
	if err != nil || index < 1 || index > len(candidateFacts) {
		return candidateChoice{Model: result.Model}, nil
	}
	return candidateChoice{Index: index, Model: result.Model}, nil
}

var judgmentPolicyForCandidates = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}

func (p cashPlan) canReconcileCandidates() bool {
	return p.review == "TRANSFER_RECONCILIATION" && len(p.candidates) > 1 && len(p.candidates) <= 10 && p.existing == ""
}

// reconcileSemantically asks if the one remaining candidate is the same event; many go to Review.
// The candidate's canonical UUID is never sent.
func (p *Processor) reconcileSemantically(ctx context.Context, requestID, amount string, at string, description string, candidateType, candidatePurpose, candidateAt string) (sameEventAnswer, error) {
	if p.verifier == nil {
		return sameEventAnswer{}, nil
	}
	// An identity check over extracted facts, not a semantic dimension decision, so the accepted set is
	// explicitly empty.
	ctx = judgment.WithPhaseMetadata(ctx, "OTHER_BOUNDED", ReconciliationPolicyVersion, []string{})
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
	// Only an affirmatively high Noul is a yes; the undecided middle band stays in Review since reusing the
	// wrong row silently merges two real events.
	sameEvent, decided := judgment.AcceptNoul(answer, judgment.NoulPolicy{High: 0.85, Low: 0.15})
	return sameEventAnswer{SameEvent: sameEvent && decided, Model: result.Model}, nil
}

// canReconcileSemantically requires exactly one survivor with nothing already reused.
func (p cashPlan) canReconcileSemantically() bool {
	return p.review == "TRANSFER_RECONCILIATION" && len(p.candidates) == 1 && p.existing == ""
}
