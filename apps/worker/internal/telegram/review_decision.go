package telegram

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// telegramReviewDecision builds the canonical ReviewDecision for a review created
// from a transaction (PRD 7, 37). Known facts are the ledger row Go already
// holds; the missing set and bounded choices depend on the reason, so a category
// review asks for a category and a merchant review asks for a merchant, never
// both (PRD 13.4). A lookup failure yields the zero decision and the caller skips
// the write rather than storing a half-built contract.
func telegramReviewDecision(ctx context.Context, tx pgx.Tx, transactionID, reviewType string) reviewdec.Decision {
	decision := reviewdec.Decision{
		Version:         reviewdec.Version,
		Subject:         reviewdec.Subject{Type: "transaction", ID: transactionID},
		ReasonCode:      reviewType,
		DecisionClass:   reviewDecisionClass(reviewType),
		KnownFacts:      map[string]any{},
		MissingFacts:    []string{},
		EvidenceRefs:    []reviewdec.EvidenceRef{{Kind: "transaction", ID: transactionID}},
		DecisionSource:  reviewdec.SourceDeterministicPlusJev,
		PolicyVersion:   judgmentPolicyVersion,
		WhyNotAuto:      reviewWhyNotAuto(reviewType),
		InteractionMode: reviewInteractionMode(reviewType),
	}
	var amount, transactionAt string
	if err := tx.QueryRow(ctx, "SELECT COALESCE(amount::text,''),COALESCE(transaction_at::text,'') FROM transaction WHERE id=$1::uuid", transactionID).Scan(&amount, &transactionAt); err != nil {
		return decision
	}
	if amount != "" {
		decision.KnownFacts["amount_idr"] = amount
	}
	if transactionAt != "" {
		decision.KnownFacts["transaction_at"] = transactionAt
	}
	switch reviewType {
	case "UNKNOWN_MERCHANT":
		decision.MissingFacts = []string{"merchant"}
		decision.AllowedActions = []string{"SET_MERCHANT", "IGNORE"}
		decision.InteractionMode = reviewdec.ModeSingleField
	case "AMBIGUOUS_CATEGORY":
		decision.MissingFacts = []string{"category"}
		decision.AllowedActions = []string{"SET_CATEGORY", "IGNORE"}
	case "UNKNOWN_PURPOSE", "TRANSFER_CLASSIFICATION":
		decision.MissingFacts = []string{"transfer_relationship"}
		decision.AllowedActions = []string{"CLASSIFY_TRANSFER", "IGNORE"}
	case "POSSIBLE_DUPLICATE":
		decision.MissingFacts = []string{"duplicate_relationship"}
		decision.AllowedActions = []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}
		decision.InteractionMode = reviewdec.ModeConflictResolution
		decision.DecisionClass = reviewdec.ClassDuplicateAmbiguity
	default:
		decision.AllowedActions = []string{"IGNORE"}
	}
	return decision
}

func reviewDecisionClass(reviewType string) string {
	switch reviewType {
	case "POSSIBLE_DUPLICATE", "CONFLICTING_EVIDENCE":
		return reviewdec.ClassDuplicateAmbiguity
	case "TRANSFER_CLASSIFICATION", "UNKNOWN_PURPOSE":
		return reviewdec.ClassHumanPolicyChoice
	default:
		return reviewdec.ClassEvidenceGap
	}
}

func reviewInteractionMode(reviewType string) string {
	if reviewType == "UNKNOWN_MERCHANT" {
		return reviewdec.ModeSingleField
	}
	return reviewdec.ModeBoundedChoice
}

func reviewWhyNotAuto(reviewType string) string {
	switch reviewType {
	case "UNKNOWN_MERCHANT":
		return "the evidence did not name a merchant and one may not be invented"
	case "AMBIGUOUS_CATEGORY":
		return "no category was decided for this merchant yet"
	case "UNKNOWN_PURPOSE", "TRANSFER_CLASSIFICATION":
		return "the transfer relationship is a human policy choice"
	case "POSSIBLE_DUPLICATE":
		return "a plausibly matching transaction already exists"
	default:
		return "the review reason has no automatic resolution"
	}
}
