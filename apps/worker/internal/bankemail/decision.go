package bankemail

import "github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"

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
		DecisionSource: reviewdec.SourceGenerativePlusJev,
		// The bounded evidence/category decision that admitted this review is ruled
		// by the verification policy, so version that rather than the tool schema.
		PolicyVersion:   BankEmailVerificationPolicyVersion,
		Provenance:      map[string]any{"pipeline": "bank-email-generic", "household": household},
		EvidenceRefs:    []reviewdec.EvidenceRef{{Kind: "source_event", ID: sourceEventID}},
		AllowedActions:  []string{"IGNORE"},
		InteractionMode: reviewdec.ModeSingleField,
	}
	switch result.ReviewType {
	case "AMBIGUOUS_CATEGORY":
		decision.DecisionClass = reviewdec.ClassEvidenceGap
		decision.MissingFacts = []string{"category"}
		decision.InteractionMode = reviewdec.ModeBoundedChoice
		decision.WhyNotAuto = "the bounded category decision did not reach a confident, well-separated choice"
	case "UNKNOWN_MERCHANT":
		decision.DecisionClass = reviewdec.ClassEvidenceGap
		decision.MissingFacts = []string{"category"}
		decision.InteractionMode = reviewdec.ModeBoundedChoice
		decision.WhyNotAuto = "the email did not name a merchant, so no stored category could be applied"
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
