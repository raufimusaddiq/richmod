package telegram

import (
	"context"
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
	"SALARY_INTERACTION",
	"MERCHANT_LEARNING_INTERACTION",
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
	"SALARY_INTERACTION":            "answer a pending payslip choice",
	"MERCHANT_LEARNING_INTERACTION": "answer a merchant rule confirmation",
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

func (p *Processor) tryJudgmentFastPath(ctx context.Context, sourceID, householdID string, update telegramUpdate, text string, now time.Time, state turnAgentContextState) (bool, error) {
	if p.judgment == nil {
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
		// Provider failure is not semantic uncertainty (PRD §9). READs may still
		// degrade to a generative READ-only turn; mutation lanes must not.
		p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeProviderFailure)
		return p.degradeWithoutJudgment(ctx, sourceID, householdID, update, text, now, state)
	}
	answer, ok := result.Answers["route"]
	if !ok || !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(judgmentRouteCriteria), judgmentPolicy.Route) || !contains(judgmentRoutes, answer.Choice) {
		p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeClarification)
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Permintaannya belum cukup jelas. Coba sebutkan arus kas, pengeluaran, tabungan, atau wealth.")
	}
	p.metrics.recordDecision(ctx, judgmentTaskRoute, judgmentOutcomeAccepted)
	// Only the aggregate READ routes consume a reporting period. Every other
	// route must keep working when the period is CUSTOM_OR_UNCLEAR.
	var period assistantRange
	if answer.Choice == "READ_SPENDING" || answer.Choice == "READ_CASHFLOW" || answer.Choice == "READ_SAVINGS" {
		var periodOK bool
		period, periodOK = p.resolveJudgmentPeriod(ctx, householdID, now, result.Answers["period"])
		if !periodOK {
			return false, nil
		}
	}
	switch answer.Choice {
	case "CREATE_TRANSACTION":
		return p.finishJudgmentSimpleTransaction(ctx, sourceID, householdID, update, text, now, result, candidate)
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
	case "OUT_OF_SCOPE":
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Saya hanya membantu pencatatan, pencarian, koreksi, arus kas, dan review keuangan keluarga.")
	default:
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Permintaannya belum cukup jelas. Coba jelaskan lagi dengan lebih spesifik.")
	}
}

// initialJudgmentRequest bundles every bounded question that can be answered
// from one shared server-state snapshot: route, reporting period, and — when Go
// already harvested exactly one amount candidate — the transaction sub-bundle.
// Speculative transaction answers are ignored when the route is unrelated.
func (p *Processor) initialJudgmentRequest(text string, state turnAgentContextState, candidate simpleTransactionCandidate) judgment.Request {
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
		statePayload["date_reference"] = candidate.DateRef
		statePayload["merchant"] = candidate.Merchant
		for key, question := range transactionQuestions(state.Categories, "") {
			questions[key] = question
		}
	}
	return judgment.Request{State: statePayload, Questions: questions}
}

// judgmentUnavailableReason is the explicit product state used when the initial
// bounded call fails. It distinguishes infrastructure failure from semantic
// uncertainty for telemetry and for the user-facing response.
const judgmentUnavailableReason = "JUDGMENT_UNAVAILABLE"

// degradeWithoutJudgment handles a provider failure on the initial call. READs
// fall through to the generative agent with a READ-only tool surface; mutation
// requests never reach a mutation tool, so no hidden LLM authority appears.
func (p *Processor) degradeWithoutJudgment(ctx context.Context, sourceID, householdID string, update telegramUpdate, text string, now time.Time, state turnAgentContextState) (bool, error) {
	if !readOnlyFallbackRequest(text) {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Permintaan ini belum dicatat karena layanan keputusan sedang tidak tersedia. Coba lagi sebentar lagi.")
	}
	return false, nil
}

// readOnlyFallbackRequest reports whether a message is clearly a read-only
// finance question. Anything else (including every mutation wording) fails
// closed while the judgment plane is unavailable.
func readOnlyFallbackRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, token := range []string{"berapa", "total", "lihat", "tampilkan", "tunjukkan", "cari", "list", "tren", "insight", "net worth", "tabungan", "pengeluaran", "pemasukan", "arus kas", "how much", "show", "list", "find", "summary"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// finishJudgmentSimpleTransaction consumes transaction answers that were
// returned by the initial bundle. It performs no second System One call and no
// generative call (PRD §10).
func (p *Processor) finishJudgmentSimpleTransaction(ctx context.Context, sourceID, householdID string, update telegramUpdate, text string, now time.Time, result judgment.Result, candidate simpleTransactionCandidate) (bool, error) {
	if candidate.Amount == "" {
		return false, nil
	}
	decision := transactionDecisionFromAnswers(result, candidate, p.categoriesOrEmpty(ctx, householdID))
	if !decision.decisionAllowed() {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Transaksi ini belum bisa dicatat otomatis. Coba sebutkan nominal, waktu, dan jenisnya lebih jelas.")
	}
	resolved, err := resolveTransactionTime(now, &candidate.DateRef, stringPtr(candidate.ExplicitDate), nil)
	if err != nil {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Waktu transaksi belum jelas.")
	}
	return true, p.persistTransaction(ctx, sourceID, householdID, update, validatedExtraction{
		Type: decision.TransactionType, Amount: candidate.Amount, TransactionAt: resolved.At,
		Merchant: candidate.Merchant, Description: candidate.Merchant, CategorySlug: decision.CategorySlug,
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
	decision.MaterialAmbiguity = noulSupported(result.Answers, "material_ambiguity", judgmentPolicy.Ambiguity)
	if decision.TransactionType == "EXPENSE" && len(categories) > 0 {
		if categoryAnswer, exists := result.Answers["category"]; exists && categoryAnswer.Choice != "OTHER_OR_UNCLEAR" && judgment.AcceptChoice(categoryAnswer, judgment.CategoryCriteria(categories), judgmentPolicy.Category) && contains(categories, categoryAnswer.Choice) {
			decision.CategorySlug, decision.CategoryAccepted = categoryAnswer.Choice, true
		}
	}
	return decision
}

var simpleAmountPattern = regexp.MustCompile(`(?i)(?:^|\s)([0-9][0-9.,]*)\s*(rb|ribu|jt|juta)?(?:\s|$)`)
var simpleDatePattern = regexp.MustCompile(`\b(20[0-9]{2}-[0-9]{2}-[0-9]{2})\b`)

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
	case "rb", "ribu":
		value.Mul(value, big.NewInt(1000))
	case "jt", "juta":
		value.Mul(value, big.NewInt(1000000))
	}
	if len(value.String()) > 20 {
		return simpleTransactionCandidate{}, false
	}
	dateRef, explicit := "TODAY", ""
	lower := strings.ToLower(text)
	if strings.Contains(lower, "kemarin") || strings.Contains(lower, "yesterday") {
		dateRef = "YESTERDAY"
	}
	if date := simpleDatePattern.FindStringSubmatch(text); len(date) == 2 {
		dateRef, explicit = "EXPLICIT", date[1]
	}
	merchant := simpleMerchant(text, matches[0][0], dateRef, explicit)
	return simpleTransactionCandidate{Amount: value.String(), DateRef: dateRef, ExplicitDate: explicit, Merchant: merchant}, true
}

func simpleMerchant(text, amountToken, dateRef, explicitDate string) string {
	value := strings.TrimSpace(strings.Replace(text, amountToken, " ", 1))
	if explicitDate != "" {
		value = strings.Replace(value, explicitDate, " ", 1)
	}
	for _, phrase := range []string{"hari ini", "kemarin", "today", "yesterday", "catat", "tambah", "simpan", "pengeluaran", "pemasukan", "expense", "income"} {
		value = strings.Replace(strings.ToLower(value), phrase, " ", 1)
	}
	value = strings.Join(strings.Fields(value), " ")
	return clean(value, 160)
}
