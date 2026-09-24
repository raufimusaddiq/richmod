package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// errNoBoundedActions marks a review reason whose question is free text. There is
// no honest bounded action vocabulary for it, so the contract is omitted.
var errNoBoundedActions = errors.New("review reason has no bounded action vocabulary")

// telegramReviewDecision builds the canonical ReviewDecision for a review created
// from a transaction (PRD 7, 37). Known facts are the ledger row Go already
// holds; the missing set and bounded choices depend on the reason, so a category
// review asks for a category and a merchant review asks for a merchant, never
// both (PRD 13.4).
//
// A reason whose question is free text has no bounded action vocabulary and gets
// no decision at all: the row is left without a contract rather than storing a
// wrong one (PRD 7.5, ADR-039).
func telegramReviewDecision(ctx context.Context, tx pgx.Tx, transactionID, reviewType string) (reviewdec.Decision, error) {
	allowed := telegramReviewActions(reviewType)
	if len(allowed) == 0 {
		return reviewdec.Decision{}, errNoBoundedActions
	}
	decision := reviewdec.Decision{
		Version:         reviewdec.Version,
		Subject:         reviewdec.Subject{Type: "transaction", ID: transactionID},
		ReasonCode:      reviewType,
		DecisionClass:   reviewDecisionClass(reviewType),
		KnownFacts:      map[string]any{},
		MissingFacts:    []string{},
		EvidenceRefs:    []reviewdec.EvidenceRef{{Kind: "transaction", ID: transactionID}},
		DecisionSource:  reviewdec.SourceDeterministic,
		PolicyVersion:   judgmentPolicyVersion,
		WhyNotAuto:      reviewWhyNotAuto(reviewType),
		InteractionMode: reviewInteractionMode(reviewType),
		AllowedActions:  allowed,
	}
	var amount, transactionAt string
	if err := tx.QueryRow(ctx, "SELECT COALESCE(amount::text,''),COALESCE(transaction_at::text,'') FROM transaction WHERE id=$1::uuid", transactionID).Scan(&amount, &transactionAt); err != nil {
		return reviewdec.Decision{}, err
	}
	if amount != "" {
		decision.KnownFacts["amount_idr"] = amount
	}
	if transactionAt != "" {
		decision.KnownFacts["transaction_at"] = transactionAt
	}
	switch reviewType {
	case "UNKNOWN_MERCHANT":
		decision.MissingFacts = []string{"category"}
	case "AMBIGUOUS_CATEGORY":
		decision.MissingFacts = []string{"category"}
		decision.DecisionSource = reviewdec.SourceDeterministicPlusJev
	case "TRANSFER_CLASSIFICATION":
		decision.MissingFacts = []string{"transfer_relationship"}
	case "POSSIBLE_DUPLICATE":
		decision.MissingFacts = []string{"duplicate_relationship"}
	}
	return decision, nil
}

// telegramReviewActions mirrors review.canonicalActions for the review reasons a
// Telegram-created review can carry, plus the Telegram reply vocabulary. The
// resolver in apps/api/internal/review only accepts these; inventing an action
// name here produces a review no client can resolve.
func telegramReviewActions(reviewType string) []string {
	switch reviewType {
	case "UNKNOWN_MERCHANT", "AMBIGUOUS_CATEGORY":
		return []string{"review:category", "review:ignore"}
	case "POSSIBLE_DUPLICATE":
		// A possible duplicate is resolved from the Telegram card by giving it a
		// category or ignoring it; there is no merge/new-transfer callback in this
		// lane, so the contract carries tokens the ingress actually accepts.
		return []string{"review:category", "review:ignore"}
	case "TRANSFER_CLASSIFICATION":
		return []string{"review:expense", "review:asset", "review:own", "review:household"}
	case "WEALTH_OBSERVATION_CONFIRMATION":
		return []string{"PREPARE_SNAPSHOT", "SET_WEALTH_ACCOUNT", "IGNORE"}
	case "FINANCIAL_EMAIL_RESOLUTION":
		return []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"}
	case "CYCLE_RESIDUAL_ALLOCATION":
		return []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"}
	case "PAYSLIP_CONFIRMATION":
		return []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}
	case "MISSING_PAY_DATE":
		return []string{"SET_PAY_DATE", "IGNORE"}
	case "UNKNOWN_BANK_TEMPLATE", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE":
		return []string{"COMPLETE_BANK_FACTS", "IGNORE"}
	default:
		// The question is free text; there is no bounded action to offer.
		return nil
	}
}

func reviewDecisionClass(reviewType string) string {
	switch reviewType {
	case "POSSIBLE_DUPLICATE", "CONFLICTING_EVIDENCE":
		return reviewdec.ClassDuplicateAmbiguity
	case "TRANSFER_CLASSIFICATION", "UNKNOWN_PURPOSE":
		return reviewdec.ClassHumanPolicyChoice
	case "CORRECTION_CONFIRMATION", "MANUAL_CORRECTION":
		return reviewdec.ClassCorrectionConfirmation
	default:
		return reviewdec.ClassEvidenceGap
	}
}

func reviewInteractionMode(reviewType string) string {
	if reviewType == "UNKNOWN_MERCHANT" {
		return reviewdec.ModeSingleField
	}
	if reviewType == "POSSIBLE_DUPLICATE" {
		return reviewdec.ModeConflictResolution
	}
	return reviewdec.ModeBoundedChoice
}

func reviewWhyNotAuto(reviewType string) string {
	switch reviewType {
	case "UNKNOWN_MERCHANT":
		return "no supported merchant category was available; choose the transaction category without inventing a merchant"
	case "AMBIGUOUS_CATEGORY":
		return "no category was decided for this merchant yet"
	case "TRANSFER_CLASSIFICATION", "UNKNOWN_PURPOSE":
		return "the transfer relationship is a human policy choice"
	case "POSSIBLE_DUPLICATE":
		return "a plausibly matching transaction already exists"
	default:
		return "the review reason has no automatic resolution"
	}
}
