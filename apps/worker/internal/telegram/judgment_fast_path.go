package telegram

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

var judgmentRoutes = []string{
	"READ_SPENDING",
	"READ_CASHFLOW",
	"READ_SAVINGS",
	"READ_WEALTH",
	"SEARCH_TRANSACTIONS",
	"CREATE_TRANSACTION",
	"CREATE_TRANSFER",
	"CORRECT_TRANSACTION",
	"REVIEW_INTERACTION",
	"PENDING_ACTION_INTERACTION",
	"SALARY_INTERACTION",
	"MERCHANT_LEARNING_INTERACTION",
	"PENDING_BATCH_INTERACTION",
	"FINANCE_HELP",
	"NEEDS_GENERATIVE_AGENT",
	"OUT_OF_SCOPE",
	"OTHER_OR_UNCLEAR",
}

// judgmentRouteCriteria is the model-visible description of every allowed
// route. The server defines the possibility space; Jev only picks inside it.
var judgmentRouteCriteria = map[string]string{
	"READ_SPENDING":                 "expense totals or breakdown",
	"READ_CASHFLOW":                 "income, outflow, or net cashflow",
	"READ_SAVINGS":                  "savings transferred or allocated",
	"READ_WEALTH":                   "net worth or wealth accounts",
	"SEARCH_TRANSACTIONS":           "find a specific transaction",
	"CREATE_TRANSACTION":            "record one income or expense",
	"CREATE_TRANSFER":               "record a transfer between accounts",
	"CORRECT_TRANSACTION":           "change an existing transaction",
	"REVIEW_INTERACTION":            "list or act on review items",
	"PENDING_ACTION_INTERACTION":    "answer a pending correction confirmation",
	"SALARY_INTERACTION":            "answer a pending payslip choice",
	"MERCHANT_LEARNING_INTERACTION": "answer a merchant rule confirmation",
	"PENDING_BATCH_INTERACTION":     "confirm, cancel, or update the pending transaction batch",
	"FINANCE_HELP":                  "examples of what Richmod can do",
	"NEEDS_GENERATIVE_AGENT":        "arbitrary extraction, reasoning, or prose is required",
	"OUT_OF_SCOPE":                  "not a household finance request",
	"OTHER_OR_UNCLEAR":              "no safe route",
}

var judgmentPeriodCriteria = map[string]string{
	"TODAY":             "today",
	"THIS_WEEK":         "this week",
	"LAST_WEEK":         "last week",
	"THIS_MONTH":        "this month",
	"LAST_MONTH":        "last month",
	"CURRENT_CYCLE":     "current salary cycle",
	"PREVIOUS_CYCLE":    "previous salary cycle",
	"CUSTOM_OR_UNCLEAR": "explicit dates or no period stated",
}

func (p *Processor) tryJudgmentFastPath(ctx context.Context, sourceID, householdID string, update telegramUpdate, text string, now time.Time, state *turnAgentContextState) (bool, error) {
	// Explicit replies are already bound to server-owned workflow state by
	// ProcessAgent. Generic route classification must not discard that target
	// before the bound agent lane interprets the reply (PRD §8.3).
	if p.judgment == nil || state.ExactReply {
		return false, nil
	}
	// Harvest generic candidates before the call so a common transaction can be
	// decided inside the same System One request as the route (PRD §10).
	candidate, harvested := harvestSimpleTransaction(text)
	if !harvested || !state.harvestable() {
		candidate = simpleTransactionCandidate{}
	}
	request := p.initialJudgmentRequest(text, state, candidate)
	result, err := p.evaluate(ctx, judgmentTaskRoute, sourceID, request)
	if err != nil {
		// Provider failure is infrastructure state, not user uncertainty.
		// The agent retains conversation and READ tools, not mutation authority.
		p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeProviderFailure)
		return false, nil
	}
	answer, ok := result.Answers["route"]
	if !ok || !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(judgmentRouteCriteria), judgmentPolicy.Route) || !contains(judgmentRoutes, answer.Choice) {
		// A failed/undecided bounded route is not permission to declare the
		// user's sentence unclear. Drop to the conversational agent with no
		// route recorded, so no implicit workflow binding is narrowed and the
		// capability policy decides what the model may do (SAVR PRD §3.3, ADR-045).
		p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeRejected)
		return false, nil
	}
	lane, knownRoute := laneForRoute(answer.Choice)
	if !knownRoute {
		// Choice validation and route-lane coverage are independent guards. A
		// vocabulary/table mismatch must not cause a guessed action, but it is
		// also not a reason to terminate the user: drop to the agent unbound.
		p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeRejected)
		return false, nil
	}
	p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeAccepted)
	// Record the decided route for the caller: implicit workflow bindings are
	// narrowed only when the route names their interaction (ADR-038 amendment).
	state.Route = answer.Choice
	// Only the aggregate READ routes consume a reporting period. Every other
	// route must keep working when the period is CUSTOM_OR_UNCLEAR.
	var period assistantRange
	if answer.Choice == "READ_SPENDING" || answer.Choice == "READ_CASHFLOW" || answer.Choice == "READ_SAVINGS" {
		var periodOK bool
		period, periodOK = p.resolveJudgmentPeriod(ctx, householdID, now, result.Answers["period"])
		if !periodOK {
			// The period is a semantic dimension the bounded route left
			// unresolved. The aggregate READ tools compute exact ranges, so let
			// the conversational agent serve the read instead of terminating.
			return false, nil
		}
	}
	switch answer.Choice {
	case "CREATE_TRANSACTION":
		return p.finishJudgmentSimpleTransaction(ctx, sourceID, householdID, update, now, result, candidate, state.Categories)
	case "READ_SPENDING":
		return true, p.replySpending(ctx, sourceID, householdID, update, period)
	case "READ_CASHFLOW":
		return true, p.replyCashflow(ctx, sourceID, householdID, update, period)
	case "READ_SAVINGS":
		return true, p.replySavings(ctx, sourceID, householdID, update, period)
	case "READ_WEALTH":
		return true, p.replyWealth(ctx, sourceID, householdID, update)
	case "REVIEW_INTERACTION":
		return true, p.replyReviews(ctx, sourceID, householdID, update)
	default:
		// Any decided route that the fast path does not terminally own — including
		// CREATE_TRANSFER, SEARCH_TRANSACTIONS, CORRECT_TRANSACTION, FINANCE_HELP,
		// NEEDS_GENERATIVE_AGENT, SALARY_INTERACTION, and
		// MERCHANT_LEARNING_INTERACTION — falls through to the conversational agent
		// (PRD §8.1). Terminating them as an unclear reply is prohibited. The
		// classification is server-owned and exhaustive, so a future route cannot
		// reach this branch without also being added to the lane table.
		switch lane {
		case laneAgentFallthrough, laneWorkflow:
			return false, nil
		default:
			// Wiring defects are machine failures, never human reviews.
			return true, fmt.Errorf("unhandled fast-path route %q", answer.Choice)
		}
	}
}

// initialJudgmentRequest bundles every bounded question that can be answered
// from one shared server-state snapshot: route, reporting period, and — when Go
// already harvested exactly one amount candidate — the transaction sub-bundle.
// Speculative transaction answers are ignored when the route is unrelated.
func (p *Processor) initialJudgmentRequest(text string, state *turnAgentContextState, candidate simpleTransactionCandidate) judgment.Request {
	statePayload := map[string]any{
		"user_text":              "<untrusted_user_message>" + text + "</untrusted_user_message>",
		"allowed_routes":         judgmentRoutes,
		"allowed_category_slugs": state.Categories,
	}
	questions := map[string]judgment.Question{
		"route":  {Type: "choice", Instructions: "Choose exactly one allowed finance workflow route. Use NEEDS_GENERATIVE_AGENT when arbitrary extraction, reasoning, or prose is required.", Criteria: judgment.ChoiceCriteria(judgmentRouteCriteria)},
		"period": {Type: "choice", Instructions: "Choose the time period the user asked about. Use CUSTOM_OR_UNCLEAR when the user gave explicit dates or stated no period.", Criteria: judgment.ChoiceCriteria(judgmentPeriodCriteria)},
	}
	if candidate.Amount != "" {
		statePayload["amount_candidates"] = []string{candidate.Amount}
		for key, question := range transactionQuestions(state.Categories, "") {
			questions[key] = question
		}
		questions["date_reference"] = judgment.Question{Type: "choice", Instructions: "Choose the date the transaction happened: TODAY, YESTERDAY, or EXPLICIT when the user gave a calendar date. Use OTHER_OR_UNCLEAR when the user gave no date at all.", Criteria: judgment.ChoiceCriteria(judgmentDateReferenceCriteria)}
	}
	return judgment.Request{State: statePayload, Questions: questions}
}

// finishJudgmentSimpleTransaction consumes transaction answers that were
// returned by the initial bundle. It performs no second System One call and no
// generative call (PRD §10).
func (p *Processor) finishJudgmentSimpleTransaction(ctx context.Context, sourceID, householdID string, update telegramUpdate, now time.Time, result judgment.Result, candidate simpleTransactionCandidate, categories []string) (bool, error) {
	if candidate.Amount == "" {
		return false, nil
	}
	decision := transactionDecisionFromAnswers(result, candidate, categories)
	if !decision.decisionAllowed() {
		return false, nil
	}
	// EXPLICIT is unreachable here: the harvest path never supplies an exact
	// calendar date, so the bounded answer cannot be EXPLICIT (date_support stays
	// false). A future harvested date would clear that in one place rather than
	// leaving a contradictory branch behind.
	resolved, err := resolveTransactionTime(now, &decision.DateReference, nil, nil)
	if err != nil {
		return false, nil
	}
	return true, p.persistTransaction(ctx, sourceID, householdID, update, validatedExtraction{
		Type: decision.TransactionType, Amount: candidate.Amount, TransactionAt: resolved.At,
		Description: candidate.Text, CategorySlug: decision.CategorySlug,
		TimePrecision: resolved.Precision, TimePeriod: resolved.Period,
	}, gateway.Metadata{Model: result.Model}, decision)
}

// resolveJudgmentPeriod turns the Jev period Choice into an exact server range.
// The second return reports whether a period was usable; READ routes must never
// silently substitute THIS_MONTH when the period is unclear.
func (p *Processor) resolveJudgmentPeriod(ctx context.Context, householdID string, now time.Time, answer judgment.Answer) (assistantRange, bool) {
	if !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(judgmentPeriodCriteria), judgmentPolicy.Route) {
		return assistantRange{}, false
	}
	switch answer.Choice {
	case "CURRENT_CYCLE", "PREVIOUS_CYCLE":
		rangeValue, err := p.resolveSalaryCycleRange(ctx, householdID, now, answer.Choice == "PREVIOUS_CYCLE")
		return rangeValue, err == nil
	case "CUSTOM_OR_UNCLEAR":
		return assistantRange{}, false
	default:
		rangeValue, err := resolveAssistantRange(now, strPtr(answer.Choice), nil, nil)
		return rangeValue, err == nil
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

// transactionDecisionFromAnswers maps one shared answer bundle into the single
// semantic decision object. It is used by both the harvested fast path and the
// post-extraction evaluator so neither path grows its own acceptance rules.
func transactionDecisionFromAnswers(result judgment.Result, candidate simpleTransactionCandidate, categories []string) TransactionSemanticDecision {
	decision := TransactionSemanticDecision{DecisionSource: "JEV", Model: result.Model, PolicyVersion: judgmentPolicy.Version}
	typeAnswer, ok := result.Answers["transaction_type"]
	decision.TypeAccepted = ok && judgment.AcceptChoice(typeAnswer, judgmentTypeCriteria, judgmentPolicy.Transaction) && (typeAnswer.Choice == "INCOME" || typeAnswer.Choice == "EXPENSE")
	if decision.TypeAccepted {
		decision.TransactionType = typeAnswer.Choice
	}
	decision.RouteAccepted = true
	decision.AmountSupported = noulSupported(result.Answers, "amount_support", judgmentPolicy.AmountSupport)
	decision.DateSupported = noulSupported(result.Answers, "date_support", judgmentPolicy.DateSupport)
	if answer, ok := result.Answers["date_reference"]; ok && answer.Choice != "OTHER_OR_UNCLEAR" && judgment.AcceptChoice(answer, judgment.ChoiceCriteria(judgmentDateReferenceCriteria), judgmentPolicy.Transaction) {
		decision.DateReference = answer.Choice
		decision.DateSupported = answer.Choice == "TODAY" || answer.Choice == "YESTERDAY"
	}
	decision.MaterialAmbiguity, decision.AmbiguityDecidedNotAmbiguous = ambiguityVerdict(result.Answers, "material_ambiguity", judgmentPolicy.Ambiguity)
	if decision.TransactionType == "EXPENSE" && len(categories) > 0 {
		if categoryAnswer, exists := result.Answers["category"]; exists && categoryAnswer.Choice != "OTHER_OR_UNCLEAR" && judgment.AcceptChoice(categoryAnswer, judgment.CategoryCriteria(categories), judgmentPolicy.Category) && contains(categories, categoryAnswer.Choice) {
			decision.CategorySlug, decision.CategoryAccepted = categoryAnswer.Choice, true
		}
	}
	return decision
}

// "k" is the Indonesian/English shorthand for ribu (thousand) and is spelled
// without punctuation in the PRD's canonical example ("jajan gorengan 5k").
// The suffix list is followed by a hard word boundary. Without it, a glued unit
// such as "5kg" or "5jt-an" matched the optional suffix and harvested a
// currency amount from a quantity (PRD §24 T1: only real amounts are harvested).
var simpleAmountPattern = regexp.MustCompile(`(?i)(?:^|\s)([0-9][0-9.,]*)\s*(rb|ribu|jt|juta|k)?\b(?:\s|$)`)

func harvestSimpleTransaction(text string) (simpleTransactionCandidate, bool) {
	matches := simpleAmountPattern.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 {
		return simpleTransactionCandidate{}, false
	}
	numeric := strings.ReplaceAll(strings.ReplaceAll(matches[0][1], ".", ""), ",", "")
	if numeric == "" {
		return simpleTransactionCandidate{}, false
	}
	value, ok := new(big.Int).SetString(numeric, 10)
	if !ok || value.Sign() <= 0 {
		return simpleTransactionCandidate{}, false
	}
	switch strings.ToLower(matches[0][2]) {
	case "rb", "ribu", "k":
		value.Mul(value, big.NewInt(1000))
	case "jt", "juta":
		value.Mul(value, big.NewInt(1000000))
	}
	if len(value.String()) > 20 {
		return simpleTransactionCandidate{}, false
	}
	// Go owns the exact syntactic amount candidate and nothing else. Date and
	// merchant wording is intelligence-owned: the same bundle asks Jev whether the
	// resolved date is supported from the raw text, and a model-authored typed
	// date is validated structurally rather than by Go re-reading the sentence.
	return simpleTransactionCandidate{Amount: value.String(), Text: text}, true
}
