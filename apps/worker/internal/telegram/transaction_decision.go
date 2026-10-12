package telegram

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// decisionPolicyVersion labels the deterministic acceptance contract in stored
// decision provenance.
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
	// AmbiguityDecidedNotAmbiguous records that the extraction was complete and
	// unambiguous; anything else fails closed to review.
	AmbiguityDecidedNotAmbiguous bool

	// ResidualCategory records the one named dimension a bounded rescue still had
	// to decide after a generative extraction. It is what lets telemetry tell a
	// valid residual rescue apart from a redundant re-decide.
	ResidualCategory bool

	DecisionSource string // JEV | DETERMINISTIC_POLICY | GENERATIVE_EXTRACTION | GENERATIVE_PLUS_JEV
	Model          string
	PolicyVersion  string
}

// decisionAllowed reports whether Go may persist CONFIRMED from this decision.
// A transaction needs a supported amount, a supported date, an accepted
// direction, and no material ambiguity. EXPENSE additionally needs an accepted
// category because category is user-visible ledger data.
func (d TransactionSemanticDecision) decisionAllowed() bool {
	if !d.RouteAccepted || !d.TypeAccepted || !d.AmountSupported || !d.DateSupported {
		return false
	}
	// The ambiguity question is inverted, so only an affirmative *negative* clears
	// it. An exact merchant-category match is decided by deterministic policy
	// without ever asking the plane, and that path is already unambiguous by
	// construction, so it is exempt rather than forced to answer a question it
	// never asked.
	if d.DecisionSource != "DETERMINISTIC_POLICY" && !d.AmbiguityDecidedNotAmbiguous {
		return false
	}
	if d.TransactionType == "INCOME" {
		return true
	}
	return d.TransactionType == "EXPENSE" && d.CategoryAccepted && strings.TrimSpace(d.CategorySlug) != ""
}

// semanticDecisionForRecord rules on one record_transaction call with Go
// validation alone (ADR-052). A complete extraction is accepted directly; any
// unresolved dimension (an unknown category, say) is recorded as residual so the
// transaction is parked for review rather than guessed.
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
	// When the category is the only open dimension, the type, amount and date are
	// still accepted facts: the review asks for the category, not for a date the
	// household already gave. CategoryAccepted stays false, so nothing is confirmed.
	if len(state.ResidualDimensions) == 1 && state.ResidualDimensions[0] == "category" {
		if complete, ok := directAcceptanceDecision(withCategory(value, categories), categories, state.Now); ok {
			decision.RouteAccepted, decision.TransactionType, decision.TypeAccepted = true, complete.TransactionType, true
			decision.AmountSupported, decision.DateSupported = true, true
		}
	}
	return decision, nil
}

// withCategory returns the extraction with its first allowed category filled in,
// only so directAcceptanceDecision can vouch for every other dimension.
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

// directAcceptanceDecision is the acceptance contract for one extracted
// transaction: the deterministic checks required before a generative result may
// be persisted as CONFIRMED. It returns false when any dimension is genuinely
// unresolved, and that transaction goes to review instead of being guessed.
func directAcceptanceDecision(value validatedExtraction, categories []string, now time.Time) (TransactionSemanticDecision, bool) {
	// Schema/type and amount format are re-asserted here rather than trusted,
	// because this function is the gate that skips the model.
	if value.Type != "INCOME" && value.Type != "EXPENSE" {
		return TransactionSemanticDecision{}, false
	}
	amount, ok := new(big.Int).SetString(value.Amount, 10)
	if !ok || amount.Sign() <= 0 || amount.String() != value.Amount {
		return TransactionSemanticDecision{}, false
	}
	// Date provenance must be explicit. OBSERVED_AT_PROCESSING means the caller
	// fell back to processing time, which is not an observed transaction time and
	// must never be canonicalized as one.
	if !acceptableDateProvenance(value.DateProvenance) || value.TransactionAt.IsZero() || now.IsZero() {
		return TransactionSemanticDecision{}, false
	}
	// Confidence is intentionally not a canonical gate. It cannot authorize an
	// event by itself, and a low/high self-score does not justify rejecting
	// a complete, schema-valid extraction.
	if value.Ambiguous {
		return TransactionSemanticDecision{}, false
	}
	// Category: an accepted active household slug is required for EXPENSE. An
	// absent or inactive slug is a genuine residual, not a reason to guess.
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

// unresolvedTransactionDimensions names exactly the residual facts that still
// block canonical confirmation.
func unresolvedTransactionDimensions(typ string, categoryKnown bool) []string {
	if typ == "EXPENSE" && !categoryKnown {
		return []string{"category"}
	}
	return nil
}

// acceptableDateProvenance reports whether the resolved date came from source
// evidence or an explicit user statement, rather than the processing fallback.
func acceptableDateProvenance(provenance string) bool {
	return provenance == "USER_STATED"
}

// recordJudgmentDecision persists bounded decision provenance in the same
// transaction as the canonical mutation, so a decision can never be recorded
// without its outcome. Only bounded decision values are stored —
// never raw user text, email bodies, or model state.
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
