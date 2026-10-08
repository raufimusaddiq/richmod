package telegram

import (
	"context"
	"slices"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/merchantmemory"
)

// tryJudgmentBoundWorkflow replaces bounded replies to server-owned workflows.
// It deliberately handles only choices that need no extra free-form facts;
// everything else stays on the conversational extraction path.
func (p *Processor) tryJudgmentBoundWorkflow(ctx context.Context, state *agentState, text string) (bool, error) {
	if state.ReviewBinding != nil && state.ReviewBinding.Kind == "TRANSACTION" && (state.ReviewBinding.ConversationState == "AWAITING_MERCHANT" || (state.ReviewBinding.ReviewType == "UNKNOWN_MERCHANT" && state.ReviewBinding.MerchantID == "")) {
		match, err := merchantmemory.Lookup(ctx, p.pool, state.HouseholdID, text)
		if err != nil {
			return true, err
		}
		if match != nil {
			result, _, err := p.agentResolveBoundReview(ctx, state, gateway.ToolCall{Name: "resolve_review", CallID: "merchant-memory"}, map[string]any{"action": "CONFIRM", "merchant": text})
			if err != nil {
				return true, err
			}
			return true, p.finishAgentText(ctx, state, agentMutationFallback(result))
		}
	}
	if p.judgment == nil {
		return false, nil
	}
	if state.HasPendingAction && state.Route == "PENDING_ACTION_INTERACTION" {
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskPendingAction, text, "pending_action", "Choose the user's bounded response to the pending correction.", map[string]any{"CONFIRM": "save the pending correction", "CANCEL": "discard the pending correction", "OTHER_OR_UNCLEAR": "no bounded action"})
		if err != nil {
			// Machine failure is not semantic uncertainty: leave the pending
			// correction untouched and let the turn fall through to the
			// conversational agent instead of asking the household to re-state it.
			return false, nil
		}
		if !ok || choice == "OTHER_OR_UNCLEAR" {
			// A bounded answer that does not address the correction means this turn
			// was not about it: keep it pending, keep answering the user.
			return false, nil
		}
		return true, p.finishPendingAction(ctx, state.HouseholdID, state.Update, state.SourceEventID, choice == "CONFIRM")
	}
	if state.HasPendingBatch && state.Route == "PENDING_BATCH_INTERACTION" {
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskPendingBatch, text, "pending_batch", "Choose one bounded action for the pending transaction batch.", map[string]any{"CONFIRM": "record every pending item", "CANCEL": "discard the batch", "UPDATE": "change one or more pending items", "DEFER": "decide later, keep the batch", "OTHER_OR_UNCLEAR": "no bounded action"})
		if err != nil {
			return false, nil
		}
		switch {
		case !ok || choice == "OTHER_OR_UNCLEAR", choice == "DEFER":
			return false, nil
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
	if state.HasSalaryChoice && state.Route == "SALARY_INTERACTION" {
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskSalaryChoice, text, "salary_choice", "Choose how to classify the pending payslip.", map[string]any{"PRIMARY": "the primary salary cycle income", "ORDINARY": "ordinary non-salary income", "IGNORE": "not household income", "OTHER_OR_UNCLEAR": "no bounded choice"})
		if err != nil {
			// Machine failure: keep the salary choice pending, answer normally.
			return false, nil
		}
		if !ok || choice == "OTHER_OR_UNCLEAR" {
			return false, nil
		}
		_, err = p.executePendingSalaryChoice(ctx, state.HouseholdID, state.Update, state.SourceEventID, salaryChoiceFromJudgment(choice))
		return true, err
	}
	if state.MerchantLearningBinding != nil {
		criteria := map[string]any{"REMEMBER": "consent to remember this merchant category rule", "SKIP": "do not remember the rule", "OTHER_OR_UNCLEAR": "not an answer to this confirmation"}
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskMerchantLearning, text, "merchant_learning", "Choose the user's bounded response to the pending merchant-category confirmation. Use OTHER_OR_UNCLEAR when the message is not answering this confirmation.", criteria)
		if err != nil || !ok || choice == "OTHER_OR_UNCLEAR" {
			// Provider failure or an unrelated message: never re-ask the same
			// question, never convert machine failure into human work.
			return false, nil
		}
		return true, p.resolveNativeMerchantLearning(ctx, state.SourceEventID, state.HouseholdID, state.Update, map[string]any{"remember": choice == "REMEMBER"})
	}
	if state.ReviewBinding != nil {
		// Semantic action vocabulary comes from the review type, never the
		// binding kind (what canonical subject is bound).
		allowed := reviewActionsForType(state.ReviewType)
		allowed = append(allowed, "OTHER_OR_UNCLEAR")
		choice, ok, err := p.judgmentChoice(ctx, state, judgmentTaskReviewAction, text, "review_action", "Choose one allowed action for the exact server-bound review. Do not invent facts or identifiers.", judgment.PlainCriteria(allowed))
		if err != nil || !ok {
			return false, nil
		}
		if choice == "OTHER_OR_UNCLEAR" {
			return false, nil
		}
		if !isReviewAction(state.ReviewType, choice) {
			return false, nil
		}
		if reviewActionNeedsArguments(choice) || (choice == "CONFIRM" && confirmNeedsTypedValue(state.ReviewBinding)) {
			// The action is already decided. Hand the generative extraction tool
			// only this action so the remaining freeform value (category, Wealth
			// hint, pay date, bank facts) is extracted, not re-decided, and the
			// user is never asked to restate it.
			state.TurnContext["bounded_review_action"] = choice
			narrowReviewActionTool(state.Tools, choice)
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

// confirmNeedsTypedValue reports a bound transaction review whose CONFIRM is only
// complete with a value the household typed (a merchant, a date, a purpose). It
// asks the executor rule itself, so Jev and the executor cannot disagree about
// which cards need a value. Jev decides the action; the generative extraction
// supplies the value.
func confirmNeedsTypedValue(binding *agentReviewBinding) bool {
	if binding.Kind != "TRANSACTION" {
		return false
	}
	_, _, required := requiredNativeReviewDetail(binding.ReviewType, binding.ConversationState, binding.MerchantID, "", "", "")
	return required
}

// isReviewAction accepts only a semantic action the bound review type offers.
// The binding kind never widens this vocabulary.
func isReviewAction(reviewType, action string) bool {
	return slices.Contains(reviewActionsForType(reviewType), action)
}

// narrowReviewActionTool restricts the resolve_review action enum to the single
// Jev-decided action so a generative extraction call can only supply the
// missing argument, never choose a different semantic meaning.
func narrowReviewActionTool(tools []gateway.ToolDefinition, action string) {
	for index := range tools {
		if tools[index].Name != "resolve_review" {
			continue
		}
		properties, ok := tools[index].Parameters["properties"].(map[string]any)
		if !ok {
			return
		}
		actionSchema, ok := properties["action"].(map[string]any)
		if !ok {
			return
		}
		actionSchema["enum"] = []string{action}
	}
}

// reviewActionNeedsArguments keeps freeform facts in the user's turn: Jev may
// route the action, but the typed native tool extracts its required values.
func reviewActionNeedsArguments(action string) bool {
	switch action {
	case "EXPENSE", "ASSET_PURCHASE", "SET_PAY_DATE", "COMPLETE_BANK_FACTS":
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
			"user_text":       untrustedUser(text),
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

type simpleTransactionCandidate struct {
	// Amount is an exact syntactic candidate only. Date and merchant meaning is
	// never established here; intelligence owns it. Text is the raw
	// turn, carried only so Jev can judge whether a purchase label is supported;
	// Go writes it as merchant/description only after that bounded ruling.
	Amount string
	Text   string
}

// judgmentSupported reports a decided, affirmative Noul (the harvested candidate
// is supported). Undecided middle-band answers fail closed.
func judgmentSupported(answer judgment.Answer, policy judgment.NoulPolicy) bool {
	remember, decided := judgment.AcceptNoul(answer, policy)
	return remember && decided
}

// transferPurposes is the canonical possibility space for a household-internal
// transfer purpose. Go owns the option set; the judgment plane only picks inside
// it.
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
// step of the permitted flow: Go's deterministic account and
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
