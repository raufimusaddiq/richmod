package bankemail

import (
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// materialResidual names only the bounded predicate that did not clear, and only
// when it is material to the canonical decision being attempted. Non-material
// metadata (an uncertain payment mechanism) never reaches here, so it cannot
// independently block an otherwise safe expense (SAVR-06).
func (v EvidenceVerification) materialResidual() (fact string, conflict bool, ok bool) {
	if len(v.ClaimOutcomes) == 0 {
		// No bounded ruling: nothing was evaluated, so no predicate failed. Callers
		// treat this as no-material-residual rather than defaulting to amount_idr
		// (SAVR-06, Hermes round 6).
		return "", false, false
	}
	switch {
	case v.ClaimOutcomes["amount_supported"] == "NO":
		// The email names a different amount: two evidence-supported values disagree.
		return "amount_idr", true, true
	case v.ClaimOutcomes["direction_supported"] == "NO":
		return "direction", true, true
	case v.ClaimOutcomes["transaction_observed"] == "NO":
		return "transaction_observed", true, true
	case v.ClaimOutcomes["amount_supported"] != "YES" || v.ClaimOutcomes["direction_supported"] != "YES" || v.ClaimOutcomes["transaction_observed"] != "YES":
		// The plane could not decide a material fact; it is missing, not conflicting.
		for _, fact := range []struct{ key, name string }{{"amount_supported", "amount_idr"}, {"direction_supported", "direction"}, {"transaction_observed", "transaction_observed"}} {
			if v.ClaimOutcomes[fact.key] != "YES" {
				return fact.name, false, true
			}
		}
	case v.ClaimOutcomes["semantic_grounded"] != "YES":
		// SPEND vs TRANSFER_OR_INTERNAL unresolved: a material residual the
		// household resolves, not a blind fail-closed (the deterministic policy
		// still owns which canonical class a resolved case becomes).
		return "transaction_semantics", false, true
	}
	return "", false, false
}

// reviewPolicyVersion names the policy that actually decided each review type,
// so the stored contract stays reproducible (PRD §18).
func reviewPolicyVersion(reviewType string) string {
	switch reviewType {
	case "TRANSFER_CLASSIFICATION", "UNKNOWN_PURPOSE", "UNKNOWN_MERCHANT":
		// Decided by the deterministic policy pipeline, not the bounded plane.
		return ToolSchemaVersion
	default:
		return BankEmailVerificationPolicyVersion
	}
}

// transactionReviewDecision builds the PRD §7 contract for a bank expense that
// parked a review. Which facts are missing is derived from the review reason, so
// the Inbox asks only what is genuinely unresolved: a new merchant with an
// undecided category is a category gap, not a request to re-enter amount, time,
// or direction (PRD §3.3, §9.1).
func transactionReviewDecision(household, sourceEventID string, extraction Extraction, result PolicyResult, transactionID string) reviewdec.Decision {
	known := map[string]any{}
	if amount := value(extraction.AmountIDR); amount != "" {
		known["amount_idr"] = amount
	}
	if at := timeValue(extraction.TransactionAt); at != "" {
		known["transaction_at"] = at
	}
	if extraction.Direction != nil {
		known["direction"] = *extraction.Direction
	}
	if channel := value(extraction.Channel); channel != "" {
		known["channel"] = channel
	}
	if merchant := value(extraction.Merchant); merchant != "" {
		known["merchant"] = merchant
	}

	decision := reviewdec.Decision{
		Version:        reviewdec.Version,
		Subject:        reviewdec.Subject{Type: "transaction", ID: transactionID},
		SourceEventID:  sourceEventID,
		ReasonCode:     result.ReviewType,
		KnownFacts:     known,
		DecisionSource: reviewdec.SourceDeterministic,
		// The version names the policy that actually decided this review type: the
		// bounded verification/category plane for evidence gaps, the deterministic
		// account-match policy for transfer classification (PRD §18).
		PolicyVersion:   reviewPolicyVersion(result.ReviewType),
		Provenance:      map[string]any{"pipeline": "bank-email-generic", "household": household},
		EvidenceRefs:    []reviewdec.EvidenceRef{{Kind: "source_event", ID: sourceEventID}},
		AllowedActions:  []string{"IGNORE"},
		InteractionMode: reviewdec.ModeSingleField,
	}
	switch result.ReviewType {
	case "AMBIGUOUS_CATEGORY":
		decision.DecisionClass = reviewdec.ClassEvidenceGap
		decision.MissingFacts = []string{"category"}
		decision.DecisionSource = reviewdec.SourceGenerativePlusJev
		decision.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		decision.InteractionMode = reviewdec.ModeSingleField
		decision.WhyNotAuto = "the bounded category decision did not reach a confident, well-separated choice"
	case "UNKNOWN_MERCHANT":
		decision.DecisionClass = reviewdec.ClassEvidenceGap
		decision.MissingFacts = []string{"category"}
		if value(extraction.Channel) == "DEBIT_CARD" && strings.TrimSpace(value(extraction.Merchant)) == "" {
			decision.MissingFacts = []string{"merchant", "category"}
		}
		decision.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		decision.WhyNotAuto = "no supported merchant-to-category mapping or decisive category ruling was available"
	case "TRANSFER_CLASSIFICATION":
		decision.DecisionClass = reviewdec.ClassHumanPolicyChoice
		decision.MissingFacts = []string{"transfer_relationship"}
		decision.InteractionMode = reviewdec.ModePolicyChoice
		decision.WhyNotAuto = "own-account versus expense is a human policy choice the evidence cannot decide"
	default:
		decision.DecisionClass = reviewdec.ClassEvidenceGap
		decision.MissingFacts = []string{"transaction_semantics"}
		decision.WhyNotAuto = "the email did not clearly support an ordinary spending classification"
	}
	return decision
}
