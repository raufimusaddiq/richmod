package telegram

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// decisionPolicyVersion labels the deterministic acceptance contract in stored decision provenance.
const decisionPolicyVersion = "2026-10-deterministic"

type TransactionSemanticDecision struct {
	RouteAccepted     bool
	TransactionType   string
	TypeAccepted      bool
	CategorySlug      string
	CategoryAccepted  bool
	AmountSupported   bool
	DateSupported     bool
	DateReference     string
	MaterialAmbiguity bool
	// AmbiguityDecidedNotAmbiguous records a complete, unambiguous extraction; anything else fails closed to review.
	AmbiguityDecidedNotAmbiguous bool

	// ResidualCategory records the dimension a bounded rescue still had to decide, so telemetry can tell a
	// valid residual rescue from a redundant re-decide.
	ResidualCategory bool

	DecisionSource string // JEV | DETERMINISTIC_POLICY | GENERATIVE_EXTRACTION | GENERATIVE_PLUS_JEV
	Model          string
	PolicyVersion  string
}

// decisionAllowed reports whether Go may persist CONFIRMED: needs a supported amount, date, accepted
// direction and no material ambiguity; EXPENSE also needs an accepted category.
func (d TransactionSemanticDecision) decisionAllowed() bool {
	if !d.RouteAccepted || !d.TypeAccepted || !d.AmountSupported || !d.DateSupported {
		return false
	}
	// Only an affirmative negative clears the inverted ambiguity question; an exact merchant-category match
	// is decided by deterministic policy without asking the plane, so it is exempt.
	if d.DecisionSource != "DETERMINISTIC_POLICY" && !d.AmbiguityDecidedNotAmbiguous {
		return false
	}
	if d.TransactionType == "INCOME" {
		return true
	}
	return d.TransactionType == "EXPENSE" && d.CategoryAccepted && strings.TrimSpace(d.CategorySlug) != ""
}

// Rules on one record_transaction call with Go validation alone (ADR-052): a complete extraction is accepted;
// an unresolved dimension is recorded as residual and parked for review.
func (p *Processor) semanticDecisionForRecord(_ context.Context, state *agentState, value validatedExtraction, categories []string, exactCategory bool) (TransactionSemanticDecision, error) {
	if direct, ok := directAcceptanceDecision(value, categories, state.Now); ok {
		if p.postGenerativeAutoConfirmOff {
			return TransactionSemanticDecision{DecisionSource: direct.DecisionSource, PolicyVersion: direct.PolicyVersion}, nil
		}
		if exactCategory {
			direct.DecisionSource = "DETERMINISTIC_POLICY"
		}
		return direct, nil
	}
	categoryKnown := strings.TrimSpace(value.CategorySlug) != "" && contains(categories, value.CategorySlug)
	state.ResidualDimensions = unresolvedTransactionDimensions(value.Type, categoryKnown)
	decision := TransactionSemanticDecision{DecisionSource: "GENERATIVE_EXTRACTION", PolicyVersion: decisionPolicyVersion}
	// When category is the only open dimension, type, amount and date stay accepted so the review asks only
	// for the category; CategoryAccepted stays false.
	if len(state.ResidualDimensions) == 1 && state.ResidualDimensions[0] == "category" {
		if complete, ok := directAcceptanceDecision(withCategory(value, categories), categories, state.Now); ok {
			decision.RouteAccepted, decision.TransactionType, decision.TypeAccepted = true, complete.TransactionType, true
			decision.AmountSupported, decision.DateSupported = true, true
		}
	}
	return decision, nil
}

// withCategory fills the first allowed category only so directAcceptanceDecision can vouch for every other dimension.
func withCategory(value validatedExtraction, categories []string) validatedExtraction {
	if len(categories) > 0 {
		value.CategorySlug = categories[0]
	}
	return value
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func appendIfMissing(values []string, target string) []string {
	if contains(values, target) {
		return values
	}
	return append(values, target)
}

// directAcceptanceDecision is the deterministic gate before a generative result may be persisted CONFIRMED;
// any genuinely unresolved dimension goes to review.
func directAcceptanceDecision(value validatedExtraction, categories []string, now time.Time) (TransactionSemanticDecision, bool) {
	// Schema/type and amount format are re-asserted because this function is the gate that skips the model.
	if value.Type != "INCOME" && value.Type != "EXPENSE" {
		return TransactionSemanticDecision{}, false
	}
	amount, ok := new(big.Int).SetString(value.Amount, 10)
	if !ok || amount.Sign() <= 0 || amount.String() != value.Amount {
		return TransactionSemanticDecision{}, false
	}
	// A date nobody stated may only be the message's own moment; see
	// acceptableTransactionTime.
	if !acceptableTransactionTime(value, now) || value.TransactionAt.IsZero() || now.IsZero() {
		return TransactionSemanticDecision{}, false
	}
	// Confidence is not a canonical gate: a self-score neither authorizes nor rejects a complete, schema-
	// valid extraction.
	if value.Ambiguous {
		return TransactionSemanticDecision{}, false
	}
	// Category: EXPENSE needs an accepted active household slug; an absent or inactive slug is a residual,
	// never guessed.
	categoryKnown := strings.TrimSpace(value.CategorySlug) != "" && contains(categories, value.CategorySlug)
	residual := unresolvedTransactionDimensions(value.Type, categoryKnown)
	if len(residual) == 0 {
		return TransactionSemanticDecision{
			RouteAccepted: true, TransactionType: value.Type, TypeAccepted: true,
			AmountSupported: true, DateSupported: true,
			CategorySlug: value.CategorySlug, CategoryAccepted: value.Type == "INCOME" || categoryKnown,
			AmbiguityDecidedNotAmbiguous: true, DecisionSource: "GENERATIVE_EXTRACTION",
			PolicyVersion: decisionPolicyVersion,
		}, true
	}
	return TransactionSemanticDecision{}, false
}

func unresolvedTransactionDimensions(typ string, categoryKnown bool) []string {
	if typ == "EXPENSE" && !categoryKnown {
		return []string{"category"}
	}
	return nil
}

// acceptableTransactionTime: a user-stated date always qualifies; otherwise only the message send time.
// A model-supplied date is never promoted.
func acceptableTransactionTime(value validatedExtraction, now time.Time) bool {
	if value.DateProvenance == "USER_STATED" {
		return true
	}
	return value.TimePrecision == "OBSERVED_AT_PROCESSING" && value.TransactionAt.Equal(now.Truncate(time.Minute))
}

// recordJudgmentDecision persists bounded decision provenance in the same transaction as the mutation, never
// raw user text, email bodies or model state.
func (p *Processor) recordJudgmentDecision(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, decision TransactionSemanticDecision, confirmed bool) error {
	outcome := "NEEDS_REVIEW"
	if confirmed {
		outcome = "CONFIRMED"
	}
	summary, err := json.Marshal(map[string]any{
		"transaction_type":                decision.TransactionType,
		"type_accepted":                   decision.TypeAccepted,
		"amount_supported":                decision.AmountSupported,
		"date_supported":                  decision.DateSupported,
		"category":                        decision.CategorySlug,
		"category_accepted":               decision.CategoryAccepted,
		"material_ambiguity":              decision.MaterialAmbiguity,
		"ambiguity_decided_not_ambiguous": decision.AmbiguityDecidedNotAmbiguous,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO judgment_decision(household_id,source_event_id,task,model,policy_version,question_keys,answer_summary_json,outcome) VALUES($1,$2,'TRANSACTION_SEMANTICS',NULLIF($3,''),$4,$5,$6::jsonb,$7)`, householdID, sourceEventID, decision.Model, decision.PolicyVersion, []string{"transaction_type", "amount_support", "date_support", "material_ambiguity", "category"}, string(summary), outcome)
	return err
}
