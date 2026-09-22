package telegram

import (
	"context"
	"fmt"
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
			"route": {Type: "choice", Instructions: "Choose exactly one allowed finance workflow route. Use NEEDS_GENERATIVE_AGENT when arbitrary extraction, reasoning, or prose is required."},
		},
	})
	if err != nil {
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Richmod belum bisa menentukan jenis permintaan ini. Coba jelaskan lagi dengan lebih spesifik.")
	}
	answer, ok := result.Answers["route"]
	if !ok || !judgment.AcceptChoice(answer, 0.85, 0.20) || !contains(judgmentRoutes, answer.Choice) {
		return true, p.finishWithoutTransaction(ctx, sourceID, "IGNORED", update, "Permintaannya belum cukup jelas. Coba sebutkan arus kas, pengeluaran, tabungan, atau wealth.")
	}
	if answer.Choice == "NEEDS_GENERATIVE_AGENT" || answer.Choice == "SEARCH_TRANSACTIONS" || answer.Choice == "CREATE_TRANSFER" || answer.Choice == "CORRECT_TRANSACTION" || answer.Choice == "SALARY_INTERACTION" || answer.Choice == "MERCHANT_LEARNING_INTERACTION" || answer.Choice == "FINANCE_HELP" {
		return false, nil
	}
	rangeValue, rangeErr := resolveAssistantRange(now, strPtr("THIS_MONTH"), nil, nil)
	if rangeErr != nil {
		return true, fmt.Errorf("resolve Jev fast-path range: %w", rangeErr)
	}
	switch answer.Choice {
	case "CREATE_TRANSACTION":
		return p.tryJudgmentSimpleTransaction(ctx, sourceID, householdID, update, text, now)
	case "READ_SPENDING":
		return true, p.replySpending(ctx, sourceID, householdID, update, rangeValue)
	case "READ_CASHFLOW":
		return true, p.replyCashflow(ctx, sourceID, householdID, update, rangeValue)
	case "READ_SAVINGS":
		return true, p.replySavings(ctx, sourceID, householdID, update, rangeValue)
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
