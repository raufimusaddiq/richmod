package telegram

import (
	"context"
	"math/big"
	"strings"
	"time"
	"unicode"
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
	amountSupported := userStatedAmount(value.Amount, state.Update.Message.Text) || userStatedAmountInRecentTurns(value.Amount, state)
	if direct, ok := directAcceptanceDecision(value, categories, state.Now, amountSupported); ok {
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
		decision.AmountSupported = amountSupported
		decision.DateSupported = !afterToday(value.TransactionAt, state.Now)
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

func directAcceptanceDecision(value validatedExtraction, categories []string, now time.Time, amountSupported bool) (TransactionSemanticDecision, bool) {
	if !directNonCategoryFieldsAccepted(value, now) {
		return TransactionSemanticDecision{}, false
	}
	categoryKnown := strings.TrimSpace(value.CategorySlug) != "" && contains(categories, value.CategorySlug)
	residual := unresolvedTransactionDimensions(value.Type, categoryKnown)
	if len(residual) == 0 {
		return TransactionSemanticDecision{
			RouteAccepted: true, TransactionType: value.Type, TypeAccepted: true,
			AmountSupported: amountSupported, DateSupported: !afterToday(value.TransactionAt, now),
			CategorySlug: value.CategorySlug, CategoryAccepted: value.Type == "INCOME" || categoryKnown,
			AmbiguityDecidedNotAmbiguous: true, DecisionSource: "GENERATIVE_EXTRACTION",
			PolicyVersion: decisionPolicyVersion,
		}, true
	}
	return TransactionSemanticDecision{}, false
}

func afterToday(at, now time.Time) bool {
	local := now.In(jakartaLocation())
	endOfToday := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, jakartaLocation())
	return !at.Before(endOfToday)
}

func userStatedAmountInRecentTurns(amount string, state *agentState) bool {
	recent, _ := state.TurnContext["recent_turns"].([]PublicTurn)
	for _, turn := range recent {
		if turn.Role == "USER" && userStatedAmount(amount, turn.Text) {
			return true
		}
	}
	return false
}

func userStatedAmount(amount, userText string) bool {
	fields := strings.FieldsFunc(strings.ToLower(userText), func(r rune) bool {
		return unicode.IsSpace(r) || r == '<' || r == '>'
	})
	for index := 0; index < len(fields); index++ {
		token := strings.Trim(fields[index], `.,!?()"'`)
		unit := ""
		if index+1 < len(fields) {
			if value, ok := amountUnits[strings.Trim(fields[index+1], `.,!?()"'`)]; ok {
				unit = value
			}
		}
		if value, ok := parseAmountToken(token, unit); ok && value == amount {
			return true
		}
	}
	return false
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
