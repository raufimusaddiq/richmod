package telegram

import (
	"context"
	"math/big"
	"regexp"
	"strings"
	"time"

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

func (p *Processor) tryJudgmentFastPath(ctx context.Context, sourceID, householdID string, update telegramUpdate, text string, now time.Time, state agentContextState) (bool, error) {
	if p.judgment == nil || state.HasPendingAction || state.HasPendingBatch || state.HasSalaryChoice || state.HasMerchantLearning || state.ActiveReviewCount > 0 || update.Message.ReplyToMessage != nil {
		return false, nil
	}
	result, err := p.judgment.Evaluate(ctx, sourceID, judgment.Request{
		State: map[string]any{
			"user_text":      "<untrusted_user_message>" + text + "</untrusted_user_message>",
			"allowed_routes": judgmentRoutes,
		},
		Questions: map[string]judgment.Question{
			"route":  {Type: "choice", Instructions: "Choose exactly one allowed finance workflow route. Use NEEDS_GENERATIVE_AGENT when arbitrary extraction, reasoning, or prose is required.", Criteria: judgment.ChoiceCriteria(judgmentRouteCriteria)},
			"period": {Type: "choice", Instructions: "Choose the time period the user asked about. Use CUSTOM_OR_UNCLEAR when the user gave explicit dates or stated no period.", Criteria: judgment.ChoiceCriteria(judgmentPeriodCriteria)},
		},
	})
	if err != nil {
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Richmod belum bisa menentukan jenis permintaan ini. Coba jelaskan lagi dengan lebih spesifik.")
	}
	answer, ok := result.Answers["route"]
	if !ok || !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(judgmentRouteCriteria), judgmentRoutePolicy) || !contains(judgmentRoutes, answer.Choice) {
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Permintaannya belum cukup jelas. Coba sebutkan arus kas, pengeluaran, tabungan, atau wealth.")
	}
	if answer.Choice == "NEEDS_GENERATIVE_AGENT" || answer.Choice == "SEARCH_TRANSACTIONS" || answer.Choice == "CREATE_TRANSFER" || answer.Choice == "CORRECT_TRANSACTION" || answer.Choice == "SALARY_INTERACTION" || answer.Choice == "MERCHANT_LEARNING_INTERACTION" || answer.Choice == "FINANCE_HELP" {
		return false, nil
	}
	period, periodOK := p.resolveJudgmentPeriod(ctx, householdID, now, result.Answers["period"])
	if !periodOK {
		return false, nil
	}
	switch answer.Choice {
	case "CREATE_TRANSACTION":
		return p.tryJudgmentSimpleTransaction(ctx, sourceID, householdID, update, text, now)
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

// judgmentRoutePolicy is the versioned acceptance policy for READ route choice.
var judgmentRoutePolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20}

// resolveJudgmentPeriod turns the Jev period Choice into an exact server range.
// The second return reports whether a period was usable; READ routes must never
// silently substitute THIS_MONTH when the period is unclear.
func (p *Processor) resolveJudgmentPeriod(ctx context.Context, householdID string, now time.Time, answer judgment.Answer) (assistantRange, bool) {
	if !judgment.AcceptChoice(answer, judgment.ChoiceCriteria(judgmentPeriodCriteria), judgmentRoutePolicy) {
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
