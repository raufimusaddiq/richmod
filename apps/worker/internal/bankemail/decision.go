package bankemail

import "github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"

func (v EvidenceVerification) materialResidual() (fact string, conflict bool, ok bool) {
	if len(v.ClaimOutcomes) == 0 {
		return "", false, false
	}
	switch {
	case v.ClaimOutcomes["amount_supported"] == "NO":
		return "amount_idr", true, true
	case v.ClaimOutcomes["direction_supported"] == "NO":
		return "direction", true, true
	case v.ClaimOutcomes["transaction_observed"] == "NO":
		return "transaction_observed", true, true
	case v.ClaimOutcomes["amount_supported"] != "YES" || v.ClaimOutcomes["direction_supported"] != "YES" || v.ClaimOutcomes["transaction_observed"] != "YES":
		for _, fact := range []struct{ key, name string }{{"amount_supported", "amount_idr"}, {"direction_supported", "direction"}, {"transaction_observed", "transaction_observed"}} {
			if v.ClaimOutcomes[fact.key] != "YES" {
				return fact.name, false, true
			}
		}
	case v.ClaimOutcomes["semantic_grounded"] != "YES":
		return "transaction_semantics", false, true
	}
	return "", false, false
}

func (v EvidenceVerification) needsBankFactsReview() bool {
	fact, conflict, _ := v.materialResidual()
	return fact != "direction" || conflict || v.ClaimOutcomes["transaction_observed"] != "YES"
}

func reviewPolicyVersion(reviewType string) string {
	switch reviewType {
	case "TRANSFER_CLASSIFICATION", "UNKNOWN_PURPOSE", "UNKNOWN_MERCHANT":
		return ToolSchemaVersion
	default:
		return BankEmailVerificationPolicyVersion
	}
}

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
		Version:         reviewdec.Version,
		Subject:         reviewdec.Subject{Type: "transaction", ID: transactionID},
		SourceEventID:   sourceEventID,
		ReasonCode:      result.ReviewType,
		KnownFacts:      known,
		DecisionSource:  reviewdec.SourceDeterministic,
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
		decision.MissingFacts = []string{"merchant", "category"}
		decision.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		decision.WhyNotAuto = "no supported merchant-to-category mapping or decisive category ruling was available"
	case "TRANSFER_CLASSIFICATION":
		decision.DecisionClass = reviewdec.ClassHumanPolicyChoice
		decision.MissingFacts = []string{"transfer_relationship"}
		if preset, ok := reviewdec.Preset("TRANSFER_CLASSIFICATION", "transaction", transactionID); ok {
			decision.AllowedActions = preset.AllowedActions
		}
		decision.InteractionMode = reviewdec.ModePolicyChoice
		decision.WhyNotAuto = "own-account versus expense is a human policy choice the evidence cannot decide"
	default:
		decision.DecisionClass = reviewdec.ClassEvidenceGap
		decision.MissingFacts = []string{"transaction_semantics"}
		decision.WhyNotAuto = "the email did not clearly support an ordinary spending classification"
	}
	return decision
}
