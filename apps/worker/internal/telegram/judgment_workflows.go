package telegram

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// tryJudgmentBoundWorkflow replaces bounded replies to server-owned workflows.
// It deliberately handles only choices that need no extra free-form facts;
// everything else stays on the conversational extraction path.
func (p *Processor) tryJudgmentBoundWorkflow(ctx context.Context, state *agentState, text string) (bool, error) {
	if p.judgment == nil {
		return false, nil
	}
	if state.HasPendingAction {
		choice, ok, err := p.judgmentChoice(ctx, state, text, "pending_action", "Choose the user's bounded response to the pending correction.", map[string]any{"CONFIRM": "save the pending correction", "CANCEL": "discard the pending correction", "OTHER_OR_UNCLEAR": "no bounded action"})
		if err != nil {
			return true, p.finishAgentText(ctx, state, "Richmod belum bisa menentukan konfirmasi ini dengan aman. Balas iya untuk simpan atau tidak untuk batal.")
		}
		if !ok || choice == "OTHER_OR_UNCLEAR" {
			return true, p.finishAgentText(ctx, state, "Balas iya untuk menyimpan perubahan, atau tidak untuk membatalkannya.")
		}
		return true, p.finishPendingAction(ctx, state.HouseholdID, state.Update, state.SourceEventID, choice == "CONFIRM")
	}
	if state.HasPendingBatch {
		choice, ok, err := p.judgmentChoice(ctx, state, text, "pending_batch", "Choose one bounded action for the pending transaction batch.", map[string]any{"CONFIRM": "record every pending item", "CANCEL": "discard the batch", "UPDATE": "change one or more pending items", "DEFER": "decide later, keep the batch", "OTHER_OR_UNCLEAR": "no bounded action"})
		if err != nil {
			return true, p.finishAgentText(ctx, state, "Richmod belum bisa menentukan aksi batch dengan aman. Balas iya, batal, atau jelaskan item yang ingin diubah.")
		}
		switch {
		case !ok || choice == "OTHER_OR_UNCLEAR", choice == "DEFER":
			return true, p.finishAgentText(ctx, state, "Batch masih menunggu konfirmasi. Balas iya untuk mencatat, batal untuk membatalkan, atau gunakan pesan baru untuk mengubah item.")
		case choice == "CONFIRM":
			return true, p.finishPendingBatch(ctx, state.HouseholdID, state.Update, state.SourceEventID, true)
		case choice == "UPDATE":
			// A batch update needs arbitrary replacement values, so Jev only
			// classifies the intent. Fall through to the generative
			// `update_pending_batch` path, which validates the server-bound item
			// reference and the replacement fields in Go.
			return false, nil
		default:
			return true, p.finishPendingBatch(ctx, state.HouseholdID, state.Update, state.SourceEventID, false)
		}
	}
	if state.HasSalaryChoice {
		choice, ok, err := p.judgmentChoice(ctx, state, text, "salary_choice", "Choose how to classify the pending payslip.", map[string]any{"PRIMARY": "the primary salary cycle income", "ORDINARY": "ordinary non-salary income", "IGNORE": "not household income", "OTHER_OR_UNCLEAR": "no bounded choice"})
		if err != nil {
			return true, p.finishAgentText(ctx, state, "Pilihan slip gaji belum cukup jelas. Pilih gaji utama, pemasukan biasa, atau abaikan.")
		}
		if !ok || choice == "OTHER_OR_UNCLEAR" {
			return true, p.finishAgentText(ctx, state, "Pilih gaji utama, pemasukan biasa, atau abaikan.")
		}
		_, err = p.processPendingSalaryChoice(ctx, state.HouseholdID, state.Update, state.SourceEventID, strings.ToLower(choice))
		return true, err
	}
	if state.MerchantLearningBinding != nil {
		remember, decided, err := p.judgmentNoul(ctx, state, text, "merchant_learning", "Did the user explicitly consent to remember this merchant category rule?")
		if err != nil || !decided {
			return true, p.finishAgentText(ctx, state, "Balas ya jika aturan merchant ini ingin disimpan, atau tidak jika tidak ingin disimpan.")
		}
		return true, p.resolveNativeMerchantLearning(ctx, state.SourceEventID, state.HouseholdID, state.Update, map[string]any{"remember": remember})
	}
	if state.ReviewBinding != nil {
		allowed := reviewActionsForType(state.ReviewMode)
		allowed = append(allowed, "OTHER_OR_UNCLEAR")
		choice, ok, err := p.judgmentChoice(ctx, state, text, "review_action", "Choose one allowed action for the exact server-bound review. Do not invent facts or identifiers.", judgment.PlainCriteria(allowed))
		if err != nil || !ok || choice == "OTHER_OR_UNCLEAR" {
			return true, p.finishAgentText(ctx, state, "Aksi review belum cukup jelas. Sebutkan pilihan yang ingin dijalankan.")
		}
		if !boundedReviewAction(choice) {
			return false, nil
		}
		result, _, err := p.agentResolveBoundReview(ctx, state, gateway.ToolCall{CallID: "jev-review", Name: "resolve_review"}, map[string]any{"action": choice})
		if err != nil {
			return true, err
		}
		return true, p.finishAgentText(ctx, state, agentMutationFallback(result))
	}
	return false, nil
}

func boundedReviewAction(action string) bool {
	switch action {
	case "CONFIRM", "IGNORE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "PREPARE_SNAPSHOT", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED", "PRIMARY_SALARY", "ORDINARY_INCOME":
		return true
	default:
		return false
	}
}

// judgmentServerPolicy is the versioned acceptance policy for bounded
// server-state workflows (pending actions, batches, salary, review actions).
var judgmentServerPolicy = judgment.ChoicePolicy{MinTop: 0.80, MinMargin: 0.15, MinConfidence: 0.60}

// judgmentMerchantConsentPolicy maps the merchant-consent Noul into remember /
// do-not-remember / clarify.
var judgmentConsentPolicy = judgment.NoulPolicy{High: 0.85, Low: 0.15}

// judgmentAmountSupportPolicy / judgmentDateSupportPolicy gate a harvested
// candidate before Go persists a transaction from it.
var (
	judgmentAmountSupportPolicy = judgment.NoulPolicy{High: 0.85, Low: 0.15}
	judgmentDateSupportPolicy   = judgment.NoulPolicy{High: 0.85, Low: 0.15}
)

// judgmentTransactionPolicy / judgmentCategoryPolicy are the versioned
// acceptance policies for the simple-transaction bundle and category Choice.
var (
	judgmentTransactionPolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}
	judgmentCategoryPolicy    = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}
)

// judgmentTypeCriteria is the model-visible option set for transaction type.
var judgmentTypeCriteria = map[string]any{
	"INCOME":           "money received",
	"EXPENSE":          "money spent",
	"OTHER_OR_UNCLEAR": "not safe to decide",
}

func (p *Processor) judgmentChoice(ctx context.Context, state *agentState, text, key, instructions string, criteria map[string]any) (string, bool, error) {
	result, err := p.judgment.Evaluate(ctx, state.SourceEventID, judgment.Request{
		State: map[string]any{
			"user_text":       "<untrusted_user_message>" + text + "</untrusted_user_message>",
			"workflow":        state.TurnContext["workflow_scope"],
			"active_review":   state.TurnContext["active_review"],
			"pending_batch":   state.TurnContext["pending_batch"],
			"pending_action":  state.TurnContext["pending_action"],
			"server_bound":    true,
			"allowed_choices": judgment.CriteriaLabels(criteria),
		},
		Questions: map[string]judgment.Question{key: {Type: "choice", Instructions: instructions, Criteria: criteria}},
	})
	if err != nil {
		return "", false, err
	}
	answer, ok := result.Answers[key]
	if !ok || !judgment.AcceptChoice(answer, criteria, judgmentServerPolicy) {
		return "", false, nil
	}
	if _, exists := criteria[answer.Choice]; !exists {
		return "", false, nil
	}
	return answer.Choice, true, nil
}

// judgmentNoul asks one yes/no question. The bool reports a usable decision;
// the middle band returns decided=false so callers ask for clarification.
func (p *Processor) judgmentNoul(ctx context.Context, state *agentState, text, key, instructions string) (bool, bool, error) {
	result, err := p.judgment.Evaluate(ctx, state.SourceEventID, judgment.Request{
		State: map[string]any{
			"user_text":    "<untrusted_user_message>" + text + "</untrusted_user_message>",
			"workflow":     state.TurnContext["workflow_scope"],
			"server_bound": true,
		},
		Questions: map[string]judgment.Question{key: {Type: "noul", Instructions: instructions}},
	})
	if err != nil {
		return false, false, err
	}
	answer, ok := result.Answers[key]
	if !ok {
		return false, false, nil
	}
	remember, decided := judgment.AcceptNoul(answer, judgmentConsentPolicy)
	return remember, decided, nil
}

func simpleJevState(text string, candidates map[string]any, categories []string) map[string]any {
	return map[string]any{
		"user_text":              "<untrusted_user_message>" + text + "</untrusted_user_message>",
		"amount_candidates":      candidates["amount_candidates"],
		"date_reference":         candidates["date_reference"],
		"allowed_category_slugs": categories,
	}
}

type simpleTransactionCandidate struct {
	Amount       string
	DateRef      string
	ExplicitDate string
	Merchant     string
}

func (p *Processor) tryJudgmentSimpleTransaction(ctx context.Context, sourceID, householdID string, update telegramUpdate, text string, now time.Time) (bool, error) {
	candidate, ok := harvestSimpleTransaction(text)
	if !ok || p.judgment == nil {
		return false, nil
	}
	categories, err := p.categorySlugs(ctx, householdID)
	if err != nil {
		return true, err
	}
	state := simpleJevState(text, map[string]any{"amount_candidates": []string{candidate.Amount}, "date_reference": candidate.DateRef}, categories)
	questions := map[string]judgment.Question{
		"type":           {Type: "choice", Instructions: "Choose the transaction direction. Use INCOME for money received and EXPENSE for money spent. Use OTHER_OR_UNCLEAR if ambiguous.", Criteria: judgmentTypeCriteria},
		"amount_support": {Type: "noul", Instructions: "Does the harvested amount candidate clearly belong to the transaction the user asked to record?"},
		"date_support":   {Type: "noul", Instructions: "Does the resolved date reference clearly match when this transaction happened?"},
	}
	if len(categories) > 0 {
		questions["category"] = judgment.Question{Type: "choice", Instructions: "Choose the best active expense category for this purchased item. Use OTHER_OR_UNCLEAR only when no category is safe.", Criteria: judgment.CategoryCriteria(categories)}
	}
	result, err := p.judgment.Evaluate(ctx, sourceID, judgment.Request{State: state, Questions: questions})
	if err != nil {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Richmod belum bisa menentukan transaksi ini dengan aman. Coba jelaskan lagi.")
	}
	if support, ok := result.Answers["amount_support"]; !ok || !judgmentSupported(support, judgmentAmountSupportPolicy) {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Jumlah transaksinya belum bisa dipastikan. Tulis ulang nominalnya dengan jelas.")
	}
	if support, ok := result.Answers["date_support"]; !ok || !judgmentSupported(support, judgmentDateSupportPolicy) {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Waktu transaksinya belum bisa dipastikan. Sebutkan tanggalnya dengan jelas.")
	}
	typeAnswer, ok := result.Answers["type"]
	if !ok || !judgment.AcceptChoice(typeAnswer, judgmentTypeCriteria, judgmentTransactionPolicy) || (typeAnswer.Choice != "INCOME" && typeAnswer.Choice != "EXPENSE") {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Transaksi ini belum jelas sebagai pemasukan atau pengeluaran.")
	}
	category := ""
	categoryConfidence := 1.0
	if typeAnswer.Choice == "EXPENSE" {
		categoryAnswer, exists := result.Answers["category"]
		if !exists || !judgment.AcceptChoice(categoryAnswer, judgment.CategoryCriteria(categories), judgmentCategoryPolicy) || categoryAnswer.Choice == "OTHER_OR_UNCLEAR" {
			return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Kategori pengeluaran belum cukup jelas untuk dicatat otomatis.")
		}
		category = categoryAnswer.Choice
		categoryConfidence = categoryAnswer.Confidence
	}
	resolved, err := resolveTransactionTime(now, &candidate.DateRef, stringPtr(candidate.ExplicitDate), nil)
	if err != nil {
		return true, p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Waktu transaksi belum jelas.")
	}
	return true, p.persistTransaction(ctx, sourceID, householdID, update, validatedExtraction{
		Type: typeAnswer.Choice, Amount: candidate.Amount, TransactionAt: resolved.At,
		Merchant: candidate.Merchant, Description: candidate.Merchant, CategorySlug: category,
		Confidence: typeAnswer.Confidence, CategoryConfidence: categoryConfidence,
		TimePrecision: resolved.Precision, TimePeriod: resolved.Period,
	}, gateway.Metadata{Model: result.Model})
}

// judgmentSupported reports a decided, affirmative Noul (the harvested candidate
// is supported). Undecided middle-band answers fail closed.
func judgmentSupported(answer judgment.Answer, policy judgment.NoulPolicy) bool {
	remember, decided := judgment.AcceptNoul(answer, policy)
	return remember && decided
}

func (p *Processor) resolveCategoryWithJudgment(ctx context.Context, sourceID, householdID, merchant, description string, categories []string) (string, float64, bool, error) {
	if strings.TrimSpace(merchant) != "" {
		var slug string
		err := p.pool.QueryRow(ctx, `SELECT c.slug FROM merchant_alias ma JOIN category c ON c.id=ma.default_category_id WHERE ma.household_id=$1 AND lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g')) AND ma.auto_apply AND ma.created_from_user_confirmation AND c.active LIMIT 1`, householdID, merchant).Scan(&slug)
		if err == nil {
			return slug, 1, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", 0, false, err
		}
	}
	if len(categories) == 0 {
		return "", 0, false, nil
	}
	criteria := judgment.CategoryCriteria(categories)
	result, err := p.judgment.Evaluate(ctx, sourceID, judgment.Request{
		State: map[string]any{
			"merchant":               merchant,
			"description":            description,
			"allowed_category_slugs": categories,
		},
		Questions: map[string]judgment.Question{
			"category": {Type: "choice", Instructions: "Choose the best active expense category. Use OTHER_OR_UNCLEAR when the evidence does not support a safe choice.", Criteria: criteria},
		},
	})
	if err != nil {
		return "", 0, false, err
	}
	answer, ok := result.Answers["category"]
	if !ok || answer.Choice == "OTHER_OR_UNCLEAR" || !judgment.AcceptChoice(answer, judgment.CategoryCriteria(categories), judgmentCategoryPolicy) || !contains(categories, answer.Choice) {
		return "", 0, false, nil
	}
	return answer.Choice, answer.Confidence, true, nil
}
