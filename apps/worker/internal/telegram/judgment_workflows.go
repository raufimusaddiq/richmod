package telegram

import (
	"context"
	"errors"
	"strings"

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
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskPendingAction, text, "pending_action", "Choose the user's bounded response to the pending correction.", map[string]any{"CONFIRM": "save the pending correction", "CANCEL": "discard the pending correction", "OTHER_OR_UNCLEAR": "no bounded action"})
		if err != nil {
			return true, p.finishAgentText(ctx, state, "Richmod belum bisa menentukan konfirmasi ini dengan aman. Balas iya untuk simpan atau tidak untuk batal.")
		}
		if !ok || choice == "OTHER_OR_UNCLEAR" {
			return true, p.finishAgentText(ctx, state, "Balas iya untuk menyimpan perubahan, atau tidak untuk membatalkannya.")
		}
		return true, p.finishPendingAction(ctx, state.HouseholdID, state.Update, state.SourceEventID, choice == "CONFIRM")
	}
	if state.HasPendingBatch {
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskPendingBatch, text, "pending_batch", "Choose one bounded action for the pending transaction batch.", map[string]any{"CONFIRM": "record every pending item", "CANCEL": "discard the batch", "UPDATE": "change one or more pending items", "DEFER": "decide later, keep the batch", "OTHER_OR_UNCLEAR": "no bounded action"})
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
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskSalaryChoice, text, "salary_choice", "Choose how to classify the pending payslip.", map[string]any{"PRIMARY": "the primary salary cycle income", "ORDINARY": "ordinary non-salary income", "IGNORE": "not household income", "OTHER_OR_UNCLEAR": "no bounded choice"})
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
		// A bounded choice is required here, not a Noul: an implicit binding must
		// be able to say "this message is not an answer to the confirmation", which
		// a yes/no plus an undecided band cannot express. Without it, an awaiting
		// merchant confirmation swallows every later message in the chat, the same
		// defect PR #115 fixed for the implicit review binding.
		criteria := map[string]any{"REMEMBER": "consent to remember this merchant category rule", "SKIP": "do not remember the rule", "OTHER_OR_UNCLEAR": "not an answer to this confirmation"}
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskMerchantLearning, text, "merchant_learning", "Choose the user's bounded response to the pending merchant-category confirmation. Use OTHER_OR_UNCLEAR when the message is not answering this confirmation.", criteria)
		if err != nil || !ok {
			return true, p.finishAgentText(ctx, state, "Balas ya jika aturan merchant ini ingin disimpan, atau tidak jika tidak ingin disimpan.")
		}
		if choice == "OTHER_OR_UNCLEAR" && state.WorkflowScope == string(agentWorkflowMerchantLearning) {
			state.MerchantLearningBinding = nil
			state.MerchantLearningCount = 0
			state.Tools = state.GeneralTools
			state.TurnContext["merchant_learning"] = nil
			state.TurnContext["workflow_scope"] = string(agentWorkflowGeneral)
			return false, nil
		}
		if choice == "OTHER_OR_UNCLEAR" {
			return true, p.finishAgentText(ctx, state, "Balas ya jika aturan merchant ini ingin disimpan, atau tidak jika tidak ingin disimpan.")
		}
		return true, p.resolveNativeMerchantLearning(ctx, state.SourceEventID, state.HouseholdID, state.Update, map[string]any{"remember": choice == "REMEMBER"})
	}
	if state.ReviewBinding != nil {
		allowed := reviewActionsForType(state.ReviewMode)
		allowed = append(allowed, "OTHER_OR_UNCLEAR")
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskReviewAction, text, "review_action", "Choose one allowed action for the exact server-bound review. Do not invent facts or identifiers.", judgment.PlainCriteria(allowed))
		if err != nil || !ok {
			return true, p.finishAgentText(ctx, state, "Aksi review belum cukup jelas. Sebutkan pilihan yang ingin dijalankan.")
		}
		// A chat-level (implicit) review binding must not hijack every later
		// message in the chat. Only a *decided* "this is not a review answer"
		// falls through: an error or an undecided classifier result above keeps the
		// clarification hard stop, because there is no evidence the message is a
		// new event. An explicit reply to the review message also keeps the hard
		// stop: there the user does mean to answer the review.
		if choice == "OTHER_OR_UNCLEAR" && state.WorkflowScope == string(agentWorkflowUniqueReview) {
			state.ReviewBinding = nil
			state.ReviewBindingCount = 0
			state.ReviewMode = ""
			state.Tools = state.GeneralTools
			state.TurnContext["active_review"] = nil
			state.TurnContext["workflow_scope"] = string(agentWorkflowGeneral)
			return false, nil
		}
		if choice == "OTHER_OR_UNCLEAR" {
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

// judgmentTypeCriteria is the model-visible option set for transaction type.
var judgmentTypeCriteria = map[string]any{
	"INCOME":           "money received",
	"EXPENSE":          "money spent",
	"OTHER_OR_UNCLEAR": "not safe to decide",
}

func (p *Processor) judgmentChoice(ctx context.Context, state *agentState, task judgmentTask, text, key, instructions string, criteria map[string]any) (string, bool, error) {
	result, err := p.evaluate(ctx, task, state.SourceEventID, judgment.Request{
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
	if !ok || !judgment.AcceptChoice(answer, criteria, judgmentPolicy.Server) {
		p.metrics.recordDecision(ctx, task, judgmentOutcomeClarification)
		return "", false, nil
	}
	if _, exists := criteria[answer.Choice]; !exists {
		p.metrics.recordDecision(ctx, task, judgmentOutcomeRejected)
		return "", false, nil
	}
	p.metrics.recordDecision(ctx, task, judgmentOutcomeAccepted)
	return answer.Choice, true, nil
}

// categoriesOrEmpty never fails a transaction turn on a category query error:
// the decision simply cannot authorize a category, so Go falls back to review.
func (p *Processor) categoriesOrEmpty(ctx context.Context, householdID string) []string {
	categories, err := p.categorySlugs(ctx, householdID)
	if err != nil {
		return nil
	}
	return categories
}

type simpleTransactionCandidate struct {
	Amount       string
	DateRef      string
	ExplicitDate string
	Merchant     string
}

// judgmentSupported reports a decided, affirmative Noul (the harvested candidate
// is supported). Undecided middle-band answers fail closed.
func judgmentSupported(answer judgment.Answer, policy judgment.NoulPolicy) bool {
	remember, decided := judgment.AcceptNoul(answer, policy)
	return remember && decided
}

// transferPurposes is the canonical possibility space for a household-internal
// transfer purpose. Go owns the option set; the judgment plane only picks inside
// it (PRD §13).
var transferPurposes = []string{
	"SAVINGS_TRANSFER",
	"INVESTMENT_CONTRIBUTION",
	"ASSET_PURCHASE",
	"DEBT_PRINCIPAL_PAYMENT",
	"INTERNAL_TRANSFER",
}

// transferPurposeCriteria describes each purpose for the model without naming
// any provider, product, or household-specific account.
var transferPurposeCriteria = map[string]string{
	"SAVINGS_TRANSFER":        "money moved into a savings account",
	"INVESTMENT_CONTRIBUTION": "money moved into an investment account",
	"ASSET_PURCHASE":          "money spent to acquire an asset",
	"DEBT_PRINCIPAL_PAYMENT":  "money paid to reduce a debt or loan principal",
	"INTERNAL_TRANSFER":       "a plain transfer between the household's own accounts",
	"OTHER_OR_UNCLEAR":        "no safe purpose can be decided",
}

// resolveTransferPurpose decides the canonical purpose for a transfer whose
// source and destination Go has already resolved. It is deliberately the LAST
// step of the permitted flow (PRD §13): Go's deterministic account and
// reconciliation rules run first, and only an unresolved purpose reaches the
// bounded judgment plane.
//
// candidateHints are server-owned, resolved account identities. They arrive as
// nil when that side does not exist (no transaction account, or structurally no
// destination Wealth), which is exactly why an omitted destination means a plain
// internal transfer rather than an error.
func (p *Processor) resolveTransferPurpose(ctx context.Context, requestID, description, amountIDR, sourceLabel, destinationLabel string, destinationWealthID *string) (string, float64, bool, error) {
	state := map[string]any{
		"allowed_purposes":  transferPurposes,
		"description":       description,
		"amount_idr":        amountIDR,
		"source_label":      strings.TrimSpace(sourceLabel),
		"destination_kind":  transferDestinationKind(destinationWealthID),
		"destination_label": strings.TrimSpace(destinationLabel),
	}
	criteria := judgment.ChoiceCriteria(transferPurposeCriteria)
	result, err := p.evaluate(ctx, judgmentTaskTransferPurpose, requestID, judgment.Request{
		State: state,
		Questions: map[string]judgment.Question{
			"purpose": {Type: "choice", Instructions: "Choose the single canonical purpose for this household-internal transfer. Use OTHER_OR_UNCLEAR when the evidence does not support a safe choice.", Criteria: criteria},
		},
	})
	if err != nil {
		p.metrics.recordDecision(ctx, judgmentTaskTransferPurpose, judgmentOutcomeProviderFailure)
		return "", 0, false, err
	}
	answer, ok := result.Answers["purpose"]
	if !ok || !contains(transferPurposes, answer.Choice) || !judgment.AcceptChoice(answer, criteria, judgmentPolicy.TransferPurpose) {
		p.metrics.recordDecision(ctx, judgmentTaskTransferPurpose, judgmentOutcomeClarification)
		return "", 0, false, nil
	}
	p.metrics.recordDecision(ctx, judgmentTaskTransferPurpose, judgmentOutcomeAccepted)
	return answer.Choice, answer.Confidence, true, nil
}

// transferDestinationKind reports the structural shape of the destination. It is
// deterministic server state, never a provider judgement.
func transferDestinationKind(destinationWealthID *string) string {
	if destinationWealthID == nil || strings.TrimSpace(*destinationWealthID) == "" {
		return "NONE"
	}
	return "WEALTH_ACCOUNT"
}

// exactMerchantCategory resolves a confirmed merchant rule for this merchant.
// That is deterministic server state, so the semantic decision short-circuits
// instead of paying for a bounded call — and the matched slug is the category,
// never an empty one (the rule already decided it).
func (p *Processor) exactMerchantCategory(ctx context.Context, householdID, merchant string) (string, bool, error) {
	if strings.TrimSpace(merchant) == "" {
		return "", false, nil
	}
	var slug string
	err := p.pool.QueryRow(ctx, `SELECT c.slug FROM merchant_alias ma JOIN category c ON c.id=ma.default_category_id WHERE ma.household_id=$1 AND lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g')) AND ma.auto_apply AND ma.created_from_user_confirmation AND c.active LIMIT 1`, householdID, merchant).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return slug, true, nil
}
