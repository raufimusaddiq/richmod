package bankemail

import (
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func timeValue(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func reviewPolicyVersion(_ string) string {
	return ToolSchemaVersion
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
		decision.DecisionSource = reviewdec.SourceGenerativeExtraction
		decision.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		decision.InteractionMode = reviewdec.ModeSingleField
		decision.WhyNotAuto = "extraction did not resolve an active household category"
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
