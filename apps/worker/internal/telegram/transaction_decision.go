package telegram

import (
	"context"
	"math/big"
	"strings"
	"time"
)

const decisionPolicyVersion = "2026-10-deterministic"

type TransactionSemanticDecision struct {
	RouteAccepted                bool
	TransactionType              string
	TypeAccepted                 bool
	CategorySlug                 string
	CategoryAccepted             bool
	AmountSupported              bool
	DateSupported                bool
	DateReference                string
	MaterialAmbiguity            bool
	AmbiguityDecidedNotAmbiguous bool

	ResidualCategory bool

	DecisionSource string
	Model          string
	PolicyVersion  string
}

func (d TransactionSemanticDecision) decisionAllowed() bool {
	if !d.RouteAccepted || !d.TypeAccepted || !d.AmountSupported || !d.DateSupported {
		return false
	}
	if d.DecisionSource != "DETERMINISTIC_POLICY" && !d.AmbiguityDecidedNotAmbiguous {
		return false
	}
	if d.TransactionType == "INCOME" {
		return true
	}
	return d.TransactionType == "EXPENSE" && d.CategoryAccepted && strings.TrimSpace(d.CategorySlug) != ""
}

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
	if len(state.ResidualDimensions) == 1 && state.ResidualDimensions[0] == "category" && directNonCategoryFieldsAccepted(value, state.Now) {
		decision.RouteAccepted, decision.TransactionType, decision.TypeAccepted = true, value.Type, true
		decision.AmountSupported, decision.DateSupported = true, true
	}
	return decision, nil
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

func directNonCategoryFieldsAccepted(value validatedExtraction, now time.Time) bool {
	if value.Type != "INCOME" && value.Type != "EXPENSE" {
		return false
	}
	amount, ok := new(big.Int).SetString(value.Amount, 10)
	if !ok || amount.Sign() <= 0 || amount.String() != value.Amount {
		return false
	}
	if !acceptableTransactionTime(value, now) || value.TransactionAt.IsZero() || now.IsZero() {
		return false
	}
	return !value.Ambiguous
}

func directAcceptanceDecision(value validatedExtraction, categories []string, now time.Time) (TransactionSemanticDecision, bool) {
	if !directNonCategoryFieldsAccepted(value, now) {
		return TransactionSemanticDecision{}, false
	}
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

func acceptableTransactionTime(value validatedExtraction, now time.Time) bool {
	if value.DateProvenance == "USER_STATED" {
		return true
	}
	return value.TimePrecision == "OBSERVED_AT_PROCESSING" && value.TransactionAt.Equal(now.Truncate(time.Minute))
}
