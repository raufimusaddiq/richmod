package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type validatedAgentCall struct {
	Call  gateway.ToolCall
	Class agentToolClass
	Args  map[string]any
}

type agentCallPlan struct {
	Class agentToolClass
	Calls []validatedAgentCall
}

// ProcessAgent is the Sprint 1 conversational entry point for free-text
// Telegram messages. Callbacks keep their deterministic Process path.
func (p *Processor) ProcessAgent(ctx context.Context, sourceEventID string) error {
	var householdID, payloadText, processingStatus, sourceType, parserName string
	var receivedAt time.Time
	if err := p.pool.QueryRow(ctx, `
		SELECT s.household_id,p.payload_json::text,s.processing_status,s.source_type,COALESCE(s.parser_name,''),s.received_at
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type IN ('TELEGRAM_TEXT','TELEGRAM_CALLBACK')`, sourceEventID).
		Scan(&householdID, &payloadText, &processingStatus, &sourceType, &parserName, &receivedAt); err != nil {
		return fmt.Errorf("load Telegram source event: %w", err)
	}
	if processingStatus == "PROCESSED" || processingStatus == "IGNORED" || processingStatus == "NEEDS_REVIEW" {
		// A retry of a turn whose mutation committed but whose answer was never
		// queued re-sends the answer instead of leaving the household in silence.
		if sourceType == "TELEGRAM_TEXT" && processingStatus != "IGNORED" {
			return p.recoverUnansweredTurn(ctx, sourceEventID, householdID, payloadText, parserName)
		}
		return nil
	}
	if sourceType == "TELEGRAM_CALLBACK" {
		return p.Process(ctx, sourceEventID)
	}

	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil {
		return fmt.Errorf("decode Telegram source evidence: %w", err)
	}
	if authorized, err := telegramMemberAuthorized(ctx, p.pool, update.Message.From.ID, householdID); err != nil {
		return fmt.Errorf("re-check Telegram sender: %w", err)
	} else if !authorized {
		return ignoreUnauthorizedSender(ctx, p.pool, sourceEventID)
	}
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Pesan kosong diabaikan.")
	}
	if isHelpCommand(text) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, helpMessage)
	}
	// The typing indicator covers the whole turn, the deterministic review lane
	// included, and keeps renewing until the answer is queued.
	stopTyping := p.startTyping(ctx, update.Message.Chat.ID)
	defer stopTyping()
	// An exact reply to a proposal-keyed review (payslip pay date, missing amount) is
	// answered by the deterministic review lane, whether it replies to the card, to
	// the document's upload, or to a bound evidence notice. The agent has no
	// binding for those review kinds. Only exact replies take this lane: chat state
	// alone never owns a turn, and transaction-keyed reviews stay with the agent.
	if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0 {
		// A card older than its 7-day projection window is still an open review;
		// renew it as the button lane does, so a typed answer is not refused as stale.
		if err := p.renewExpiredReviewProjection(ctx, householdID, update); err != nil {
			return err
		}
		target, err := p.replyTargetForEvidenceReview(ctx, householdID, update)
		if err != nil {
			return err
		}
		proposalReview, err := p.repliesToProposalReview(ctx, householdID, target)
		if err != nil {
			return err
		}
		if proposalReview {
			if handled, err := p.processBoundReview(ctx, sourceEventID, householdID, target); handled || err != nil {
				return err
			}
		}
	}
	ctx = withTurnIdentity(ctx, householdID, sourceEventID)
	categories, err := p.categorySlugs(ctx, householdID)
	if err != nil {
		return err
	}
	contextState, err := p.loadAgentContextState(ctx, householdID, sourceEventID, update)
	if err != nil {
		return err
	}

	// Server-owned binding precedence. An explicit reply is terminal: if it no
	// longer points at an eligible workflow, do not fall back to a different
	// active review just because that review is unique in the chat.
	explicitReply := update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0
	merchantBinding, merchantCount, err := p.loadAgentMerchantLearningBinding(ctx, householdID, update)
	if err != nil {
		return err
	}
	if !explicitReply && merchantBinding != nil && !isYesNoAnswer(text) {
		// Without a reply, a pending merchant-learning question owns the turn only
		// when the text is a plain yes/no; anything else is a new message.
		merchantBinding, merchantCount = nil, 0
	}
	var reviewBinding *agentReviewBinding
	var reviewPublic any
	reviewCount := 0
	var openCards []map[string]any
	if explicitReply {
		// Merchant learning is a distinct post-confirmation workflow and must be
		// recognized before the generic transaction-review binder.
		if merchantBinding == nil {
			reviewBinding, err = p.exactAgentReviewBinding(ctx, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID)
			if err != nil {
				return err
			}
			if reviewBinding != nil {
				reviewPublic, reviewCount = agentReviewBindingPublic(reviewBinding), 1
			}
		}
	} else {
		// R5: a typed answer without Reply binds to an open card only by
		// deterministic rules (cardanswer.go); the model never decides it.
		cards, err := p.loadLiveReviewCards(ctx, householdID, update.Message.Chat.ID)
		if err != nil {
			return err
		}
		names, err := p.categoryNames(ctx, householdID)
		if err != nil {
			return err
		}
		matched, answer := matchTypedCardAnswer(text, cards, names, p.turnTime(update))
		switch {
		case len(matched) == 1:
			reviewBinding = &matched[0]
			reviewPublic, reviewCount = agentReviewBindingPublic(reviewBinding), 1
		case len(matched) > 1:
			return p.askWhichCard(ctx, householdID, sourceEventID, update, matched, answer)
		}
		for index := range cards {
			openCards = append(openCards, agentReviewBindingPublic(&cards[index]))
		}
		if reviewBinding == nil {
			reviewCount = len(cards)
		}
	}

	// CEU: bind the evidence this turn is about. A reply to the user's upload or to a
	// bound notice binds that document (and its open review); a review binding gains
	// its evidence as context. Nothing here is chosen by the model.
	// Evidence is context. A failure to resolve it must never fail the user's
	// message: the turn proceeds without it, and an unresolved explicit reply is
	// already handled as "unbound", which asks instead of guessing.
	evidence, evidenceReview, evidenceErr := p.bindTurnEvidence(ctx, householdID, sourceEventID, update, reviewBinding, merchantBinding != nil, explicitReply)
	if evidenceErr != nil {
		evidence, evidenceReview = nil, nil
	}
	if reviewBinding == nil && evidenceReview != nil {
		reviewBinding = evidenceReview
		reviewPublic, reviewCount = agentReviewBindingPublic(reviewBinding), 1
	}
	// With no reply, no review and no pending workflow owning the turn, evidence the
	// user just sent is offered as context: bound when exactly one qualifies, a
	// bounded ambiguous set otherwise. Context only; it never narrows the tools.
	var recentEvidence []map[string]any
	// One candidates query per turn serves the context, the harvest decision and the
	// ledger guard. It is optional: a failure leaves no evidence context and no guard.
	recentCandidates, _ := p.recentEvidenceCandidates(ctx, householdID, update.Message.Chat.ID, update.Message.From.ID)
	freshDocuments := freshEvidenceDocuments(recentCandidates)
	if evidence == nil && !explicitReply && reviewBinding == nil && merchantBinding == nil &&
		!contextState.HasPendingAction && !contextState.HasPendingBatch && !contextState.HasSalaryChoice {
		evidence, recentEvidence = p.recentEvidenceContext(ctx, householdID, sourceEventID, update, recentCandidates)
		// Recent evidence is context only. It deliberately does not bind its open review:
		// a no-reply review binding is route-gated and must come from eligible chat
		// reviews, not from recent evidence alone.
	}

	contextState.ActiveReview = reviewPublic
	contextState.ActiveReviewCount = reviewCount
	contextState.ReviewType, contextState.ReviewMode = "", ""
	if bound, ok := reviewPublic.(map[string]any); ok {
		contextState.ReviewType, _ = bound["review_type"].(string)
		contextState.ReviewMode, _ = bound["review_mode"].(string)
	}
	contextState.HasMerchantLearning = merchantBinding != nil && merchantCount == 1

	now := p.turnTime(update)
	_ = p.persistTurn(ctx, householdID, sourceEventID, update, "USER", text, "", map[string]any{"current_jakarta_datetime": now.Format(time.RFC3339)})
	generalTools := agentFinanceTools(
		categories,
		contextState.HasPendingAction,
		contextState.HasPendingBatch,
		reviewBinding != nil,
		contextState.ReviewType,
		contextState.HasSalaryChoice,
		contextState.HasMerchantLearning,
		contextState.ReviewMode,
	)
	// get_evidence_context reads a ref Richmod issued with this turn's evidence;
	// without evidence context there is nothing for it to read.
	if evidence == nil && len(recentEvidence) == 0 {
		generalTools = withoutTool(generalTools, "get_evidence_context")
	}
	// Go validation is the only mutation authority (ADR-052): the agent serves
	// every turn with the tools that apply to the household's current state.
	// Exact replies narrow to their bound workflow. A reply to a bot message that
	// is not a workflow (an earlier answer, a batch list) is an ordinary turn and
	// keeps the general catalog; a reply to a closed review card stays narrowed.
	tools, workflowScope := generalTools, agentWorkflowGeneral
	staleWorkflowReply := false
	if explicitReply {
		tools, workflowScope = applyAgentWorkflowToolPolicy(generalTools, update, reviewBinding, merchantBinding)
		if workflowScope == agentWorkflowExplicitUnbound && evidence == nil {
			staleWorkflowReply, err = p.replyTargetsWorkflowMessage(ctx, householdID, update)
			if err != nil {
				return err
			}
			if !staleWorkflowReply {
				tools, workflowScope = generalTools, agentWorkflowGeneral
			}
		}
	}
	tools, workflowScope = applyEvidenceToolPolicy(generalTools, tools, workflowScope, evidence)
	focusReviewArguments(tools, reviewBinding)

	turnContext := buildAgentTurnContext(text, now, categories, contextState)
	turnContext["workflow_scope"] = string(workflowScope)
	turnContext["merchant_learning_count"] = merchantCount
	if len(openCards) > 0 {
		turnContext["open_review_cards"] = openCards
	}
	if staleWorkflowReply {
		turnContext["explicit_reply_unbound"] = true
	}
	if evidence != nil {
		turnContext["bound_evidence"] = evidence.Context
	}
	if len(recentEvidence) > 0 {
		turnContext["recent_evidence"] = recentEvidence
		turnContext["evidence_ambiguous"] = true
	}
	if merchantBinding != nil {
		turnContext["merchant_learning"] = map[string]any{"merchant": merchantBinding.Merchant, "category": merchantBinding.Category}
	}
	state := &agentState{
		SourceEventID:    sourceEventID,
		HouseholdID:      householdID,
		Update:           update,
		Now:              now,
		Categories:       categories,
		Tools:            tools,
		RequiredTool:     "",
		TurnContext:      turnContext,
		HasPendingAction: contextState.HasPendingAction,
		HasPendingBatch:  contextState.HasPendingBatch,
		HasSalaryChoice:  contextState.HasSalaryChoice,
		ReviewType:       contextState.ReviewType,
		ReviewMode:       contextState.ReviewMode,
		Analytics:        analyticscore.NewSession(p.pool, householdID, now, sourceEventID),
		// Server-only: documents the user sent in the immediate window, for the
		// record_transaction duplicate guard. Never model-visible.
		FreshEvidenceDocuments: freshDocuments,
		ReceivedAt:             receivedAt,
		activity:               new(atomic.Int32),
	}
	// An exact reply attaches its bound workflow. Otherwise a deterministic card
	// match or a yes/no merchant-learning answer is attached while the catalog
	// stays whole, so a plain chat answer still resolves the one open question.
	if workflowScope == agentWorkflowExactReview || (!explicitReply && reviewBinding != nil) {
		state.ReviewBinding = reviewBinding
		state.ReviewBindingCount = reviewCount
	}
	if workflowScope == agentWorkflowExactMerchant || (!explicitReply && merchantBinding != nil) {
		state.MerchantLearningBinding = merchantBinding
		state.MerchantLearningCount = merchantCount
	}

	agentGateway, ok := p.gateway.(conversationalGateway)
	if !ok {
		// No conversational model is wired: the message waits like any outage.
		return p.deferForGateway(ctx, state, errConversationalGatewayMissing)
	}

	turnCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.TotalTurnTimeout)
	defer cancel()
	// A turn that needs several model phases can take a while; say so once
	// instead of leaving the household to wonder. The text names what the turn
	// is doing when the notice fires.
	stopProgress := startProgressNotice(ProgressNoticeDelay, func(noticeCtx context.Context) error {
		return p.enqueueProgressNotice(noticeCtx, state.Update, progressNoticeFor(state.activityNow()))
	})
	defer stopProgress()
	return p.runAgentLoop(turnCtx, agentGateway, state)
}

func (p *Processor) startTyping(ctx context.Context, chatID int64) func() {
	if p.bot == nil || chatID == 0 {
		return func() {}
	}
	stop := make(chan struct{})
	send := func() {
		typingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_ = p.bot.Typing(typingCtx, chatID)
		cancel()
	}
	send()
	go func() {
		// Telegram shows "typing" for about 5 s; renewing every 4 s keeps it
		// continuous for the whole turn, including after a progress notice
		// (sending a message clears the indicator until the next renewal).
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				send()
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// finalPhaseInstruction is added on the last model phase of a turn that has
// gathered facts: no more tools are offered, so the answer uses what was read
// instead of the turn ending with nothing.
const finalPhaseInstruction = `

FINAL PHASE: no more tools are available in this turn. Answer the user now using only the tool results you already have. If something could not be checked, say briefly what is missing; do not promise another lookup.`

// partialAnswerMessage is the last resort when even the final phase produced
// no answer.
const partialAnswerMessage = "Aku sudah mengumpulkan sebagian datanya, tapi belum sempat merangkum semuanya dalam satu jawaban. Coba tanyakan bagian yang paling penting dulu, ya."

func (p *Processor) runAgentLoop(ctx context.Context, model conversationalGateway, state *agentState) error {
	for state.ModelPhases < defaultAgentLimits.MaxModelPhases {
		// The last phase of a turn that already read facts answers from them.
		finalPhase := state.ModelPhases == defaultAgentLimits.MaxModelPhases-1 && len(state.PendingToolOutputs) > 0
		request := gateway.AgentRequest{
			SystemPrompt:       conversationalAgentPrompt,
			Content:            agentModelContent(state),
			Tools:              state.Tools,
			AllowParallel:      true,
			PreviousResponseID: state.PreviousResponseID,
			PreviousToolCalls:  state.PreviousToolCalls,
			ToolOutputs:        state.PendingToolOutputs,
			RequiredTool:       state.RequiredTool,
		}
		if finalPhase {
			request.Tools = nil
			request.SystemPrompt += finalPhaseInstruction
		}
		phaseCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.ModelCallTimeout)
		response, err := model.AgentTurn(phaseCtx, state.SourceEventID, request)
		cancel()
		if err != nil {
			if state.SideEffects == 0 && IsGatewayUnavailable(err) {
				// The AI is unreachable and nothing was written: the message waits
				// durably and runs the full agent path when the gateway is back.
				return p.deferForGateway(ctx, state, err)
			}
			if finalPhase && !IsGatewayUnavailable(err) && !errors.Is(err, context.DeadlineExceeded) {
				return p.finishAgentFailure(ctx, state, partialAnswerMessage, "PHASE_LIMIT")
			}
			return fmt.Errorf("%w: %w", errModelPhase, err)
		}
		// Continuation data is single-use. If this response asks for another READ
		// phase, the new response/call IDs replace it below.
		state.PreviousResponseID = ""
		state.PreviousToolCalls = nil
		state.PendingToolOutputs = nil
		state.ModelPhases++

		if len(response.ToolCalls) == 0 {
			if state.RequiredTool != "" {
				return fmt.Errorf("required native tool %q was not returned", state.RequiredTool)
			}
			return p.finishAgentText(ctx, state, response.Text)
		}
		if finalPhase {
			return p.finishAgentFailure(ctx, state, partialAnswerMessage, "PHASE_LIMIT")
		}
		if state.RequiredTool != "" && (len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != state.RequiredTool) {
			return fmt.Errorf("required native tool %q was not selected", state.RequiredTool)
		}
		plan, err := validateAgentCallSet(response.ToolCalls, state, defaultAgentLimits)
		if err != nil {
			if rejected, ok := repairableArguments(state, err); ok {
				// Go rejected the arguments, not the intent. Tell the model why, once,
				// so it can correct the call or ask the user; Go validates again.
				state.ArgumentRepairs++
				slog.Default().Warn("agent tool arguments rejected; asking the model to correct them", "tool", rejected.Call.Name, "repair", state.ArgumentRepairs)
				state.PreviousResponseID = response.ResponseID
				state.PreviousToolCalls = append([]gateway.ToolCall(nil), response.ToolCalls...)
				state.PendingToolOutputs = argumentRepairOutputs(response.ToolCalls, rejected)
				continue
			}
			return p.finishAgentFailure(ctx, state, "Aku belum bisa memproses permintaan ini dengan aman, jadi belum ada yang dicatat atau diubah. Coba sampaikan lagi dengan lebih singkat, ya.", "UNSAFE_TOOL_CALL")
		}

		switch plan.Class {
		case agentToolRead:
			state.setActivity(activityAnalyzing)
			results, fresh, err := p.executeAgentReadBatch(ctx, state, plan.Calls)
			if err != nil {
				return fmt.Errorf("conversational read batch: %w", err)
			}
			state.ReadCalls += len(results)
			state.History = append(state.History, results...)
			outputs := make([]gateway.AgentToolOutput, 0, len(results))
			for index, result := range results {
				public := agentToolResultPublic(result)
				outputs = append(outputs, gateway.AgentToolOutput{CallID: result.CallID, Output: public})
				if fresh[index] {
					_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, public)
				}
			}
			state.PreviousResponseID = response.ResponseID
			state.PreviousToolCalls = append([]gateway.ToolCall(nil), response.ToolCalls...)
			state.PendingToolOutputs = outputs

		case agentToolSideEffect:
			state.setActivity(activityRecording)
			call := plan.Calls[0]
			var result agentToolResult
			var synthesize bool
			switch {
			case isAgentSpecializedSideEffect(call.Call.Name):
				result, synthesize, err = p.executeAgentSpecializedSideEffectStrict(ctx, state, call.Call, call.Args)
			case isAgentCoreSideEffect(call.Call.Name):
				result, synthesize, err = p.executeAgentSideEffect(ctx, state, call.Call, call.Args, response.Metadata)
			default:
				err = fmt.Errorf("registered side effect %q has no conversational executor", call.Call.Name)
			}
			var recoverable *agentRecoverable
			if errors.As(err, &recoverable) {
				// Go refused the call in a way the model can fix; nothing was written.
				if state.Recoveries < maxRecoverableResults && state.ModelPhases < defaultAgentLimits.MaxModelPhases {
					state.Recoveries++
					slog.Default().Warn("agent side effect refused; returning the reason to the model", "tool", call.Call.Name, "status", recoverable.Status)
					state.PreviousResponseID = response.ResponseID
					state.PreviousToolCalls = append([]gateway.ToolCall(nil), response.ToolCalls...)
					state.PendingToolOutputs = []gateway.AgentToolOutput{{CallID: call.Call.CallID, Output: recoverable.toolOutput(call.Call.Name)}}
					state.setActivity(activityThinking)
					continue
				}
				return p.finishAgentFailure(ctx, state, recoverable.UserCopy, recoverable.Status)
			}
			if err != nil {
				return fmt.Errorf("conversational side effect %s: %w", call.Call.Name, err)
			}
			state.SideEffects++
			state.History = append(state.History, result)
			_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, agentToolResultPublic(result))
			if len(state.FreshEvidenceDocuments) > 0 {
				if _, err = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent',parser_version='1'
					WHERE id=ANY(SELECT source_event_id FROM document WHERE id=ANY($1::uuid[]) UNION SELECT source_event_id FROM document_page WHERE document_id=ANY($1::uuid[]))
					AND processing_status = 'NEEDS_REVIEW'`, state.FreshEvidenceDocuments); err != nil {
					return fmt.Errorf("finalize stale evidence source: %w", err)
				}
			}
			if state.ReplyQueued {
				// The deterministic reply was queued inside the mutation.
				return nil
			}
			// A reply that lost the race to a tap, the web, or another member is
			// answered deterministically with the outcome, never by the model.
			if result.Status == "STALE_REVIEW_BINDING" {
				if answered := staleReviewReply(ctx, p.pool, state.HouseholdID, state.Update, ""); answered != "" {
					return p.finishAgentText(ctx, state, answered)
				}
			}
			// A result Go fully knows is answered deterministically; only a result
			// without its own copy is worded by the model.
			if reply, ok := agentMutationReply(result); ok || !synthesize {
				return p.finishAgentText(ctx, state, reply)
			}
			return p.synthesizeMutationResult(ctx, model, state, result)

		default:
			return p.finishAgentFailure(ctx, state, "Richmod tidak bisa menentukan aksi yang aman untuk pesan ini, jadi belum ada yang dicatat.", "NO_SAFE_ACTION")
		}
	}
	return p.finishAgentFailure(ctx, state, partialAnswerMessage, "PHASE_LIMIT")
}

// agentArgumentsError marks a tool call whose arguments Go rejected. It is the
// only validation failure the model can fix by itself: an unavailable tool, a
// budget, or a mixed call set is a server-state decision and stays final.
type agentArgumentsError struct {
	Call gateway.ToolCall
	Err  error
}

func (e *agentArgumentsError) Error() string {
	return fmt.Sprintf("validate %s: %v", e.Call.Name, e.Err)
}
func (e *agentArgumentsError) Unwrap() error { return e.Err }

// maxArgumentRepairs bounds how many times one turn may return a rejection to the
// model. One correction fixes a malformed field; a second means the model cannot.
const maxArgumentRepairs = 1

// maxRepairReasonBytes bounds Go's rejection reason handed back to the model.
// Validation messages name the expected format, so they need a little room.
const maxRepairReasonBytes = 240

// repairableArguments reports the rejection to hand back to the model, if any
// repair is still allowed in this turn.
func repairableArguments(state *agentState, err error) (*agentArgumentsError, bool) {
	var rejected *agentArgumentsError
	if !errors.As(err, &rejected) || state.ArgumentRepairs >= maxArgumentRepairs || state.ModelPhases >= defaultAgentLimits.MaxModelPhases {
		return nil, false
	}
	return rejected, true
}

// argumentRepairOutputs answers every call of the rejected response, since the
// provider requires an output per call. Only the offending call carries a reason.
func argumentRepairOutputs(calls []gateway.ToolCall, rejected *agentArgumentsError) []gateway.AgentToolOutput {
	reason := rejected.Err.Error()
	if len(reason) > maxRepairReasonBytes {
		reason = reason[:maxRepairReasonBytes]
	}
	outputs := make([]gateway.AgentToolOutput, 0, len(calls))
	for _, call := range calls {
		output := map[string]any{"status": "NOT_EXECUTED", "tool": call.Name, "next_step": "Nothing was run. Call it again once the other call is corrected."}
		if call.CallID == rejected.Call.CallID {
			output = map[string]any{
				"status": "INVALID_ARGUMENTS", "tool": call.Name, "reason": reason,
				"next_step": "Nothing was run. Correct the arguments and call the tool again, or reply in text asking the user for the missing detail. Do not guess a value.",
			}
		}
		outputs = append(outputs, gateway.AgentToolOutput{CallID: call.CallID, Output: output})
	}
	return outputs
}

func validateAgentCallSet(calls []gateway.ToolCall, state *agentState, limits agentLimits) (agentCallPlan, error) {
	if len(calls) == 0 {
		return agentCallPlan{}, fmt.Errorf("empty tool set")
	}
	validated := make([]validatedAgentCall, 0, len(calls))
	readCount, sideEffectCount := 0, 0
	var argumentErr *agentArgumentsError
	for _, call := range calls {
		if len(state.Tools) > 0 && !agentToolAvailable(state.Tools, call.Name) {
			return agentCallPlan{}, fmt.Errorf("tool %q is not available in the current server state", call.Name)
		}
		class, ok := agentToolClassFor(call.Name)
		if !ok {
			return agentCallPlan{}, fmt.Errorf("unknown agent tool %q", call.Name)
		}
		args, err := validateAgentToolCall(call)
		if err != nil {
			// Remember the first rejection but keep checking: a set-shape or budget
			// refusal is final and must win over a repairable argument error.
			if argumentErr == nil {
				argumentErr = &agentArgumentsError{Call: call, Err: err}
			}
		} else {
			validated = append(validated, validatedAgentCall{Call: call, Class: class, Args: args})
		}
		if class == agentToolRead {
			readCount++
		} else {
			sideEffectCount++
		}
	}
	if sideEffectCount > 0 {
		if sideEffectCount != 1 || len(calls) != 1 {
			return agentCallPlan{}, fmt.Errorf("side effect must be the only tool call in a model response")
		}
		if state.SideEffects >= limits.MaxSideEffectsPerTurn {
			return agentCallPlan{}, fmt.Errorf("side effect limit reached")
		}
		if argumentErr != nil {
			return agentCallPlan{}, argumentErr
		}
		return agentCallPlan{Class: agentToolSideEffect, Calls: validated}, nil
	}
	if readCount != len(calls) {
		return agentCallPlan{}, fmt.Errorf("mixed tool classes")
	}
	if readCount > limits.MaxReadCallsPerResponse || state.ReadCalls+readCount > limits.MaxReadCallsPerTurn {
		return agentCallPlan{}, fmt.Errorf("read tool budget exceeded")
	}
	if argumentErr != nil {
		return agentCallPlan{}, argumentErr
	}
	return agentCallPlan{Class: agentToolRead, Calls: validated}, nil
}

func agentToolAvailable(tools []gateway.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// executeAgentReadBatch runs a response's READ calls in parallel. An identical
// call (same tool, same arguments) already answered in this turn is served from
// the turn's cache instead of being run again; fresh[i] is false for those, so
// the stored conversation does not repeat them either.
func (p *Processor) executeAgentReadBatch(ctx context.Context, state *agentState, calls []validatedAgentCall) ([]agentToolResult, []bool, error) {
	if state.Analytics == nil {
		state.Analytics = analyticscore.NewSession(p.pool, state.HouseholdID, state.Now, state.SourceEventID)
	}
	if state.ReadCache == nil {
		state.ReadCache = map[string]agentToolResult{}
	}
	results := make([]agentToolResult, len(calls))
	fresh := make([]bool, len(calls))
	errs := make([]error, len(calls))
	keys := make([]string, len(calls))
	firstIndex := map[string]int{}
	var wg sync.WaitGroup
	for index := range calls {
		keys[index] = readCacheKey(calls[index].Call.Name, calls[index].Args)
		if _, cached := state.ReadCache[keys[index]]; cached {
			continue
		}
		if _, seen := firstIndex[keys[index]]; seen {
			continue
		}
		firstIndex[keys[index]] = index
		fresh[index] = true
		wg.Add(1)
		index := index
		go func() {
			defer wg.Done()
			prefix := fmt.Sprintf("p%dr%d", state.ModelPhases, index+1)
			results[index], errs[index] = p.executeAgentRead(ctx, state, calls[index].Call, calls[index].Args, prefix)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	for index := range calls {
		if fresh[index] {
			state.ReadCache[keys[index]] = results[index]
			continue
		}
		source, cached := state.ReadCache[keys[index]]
		if !cached {
			source = results[firstIndex[keys[index]]]
		}
		source.CallID = calls[index].Call.CallID
		results[index] = source
	}
	return results, fresh, nil
}

// readCacheKey identifies a READ call by tool and canonical arguments.
func readCacheKey(name string, args map[string]any) string {
	encoded, _ := json.Marshal(args) // map keys marshal sorted: canonical
	return name + "|" + string(encoded)
}

func (p *Processor) synthesizeMutationResult(ctx context.Context, model conversationalGateway, state *agentState, result agentToolResult) error {
	if state.ModelPhases >= defaultAgentLimits.MaxModelPhases {
		return p.finishAgentText(ctx, state, agentMutationFallback(result))
	}
	content := map[string]any{
		"instruction":                 "Write the final user-facing response for this completed finance action. Use only the authoritative result below. Do not add financial facts and do not request another action.",
		"current_user_text":           state.TurnContext["current_user_text"],
		"authoritative_action_result": result,
	}
	// On failure the deterministic fallback below still answers.
	phaseCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.ModelCallTimeout)
	response, err := model.AgentTurn(phaseCtx, state.SourceEventID, gateway.AgentRequest{SystemPrompt: conversationalAgentPrompt, Content: content})
	cancel()
	if err != nil || len(response.ToolCalls) != 0 || strings.TrimSpace(response.Text) == "" {
		return p.finishAgentText(ctx, state, agentMutationFallback(result))
	}
	state.ModelPhases++
	return p.finishAgentText(ctx, state, response.Text)
}

func agentModelContent(state *agentState) map[string]any {
	return map[string]any{
		"turn_context":        state.TurnContext,
		"agent_tool_results":  state.History,
		"supported_languages": []string{"id", "en"},
	}
}

func agentToolResultPublic(result agentToolResult) map[string]any {
	encoded, _ := json.Marshal(result)
	var out map[string]any
	_ = json.Unmarshal(encoded, &out)
	return out
}

func (p *Processor) finishAgentText(ctx context.Context, state *agentState, message string) error {
	return p.finishAgentReply(ctx, state, message, "")
}

// finishAgentFailure answers a turn that could not do what was asked. The reply
// is stored as an ASSISTANT turn marked with the failure, so the next turn knows
// the question was not answered and nothing was recorded from it.
func (p *Processor) finishAgentFailure(ctx context.Context, state *agentState, message, failure string) error {
	if failure == "" {
		failure = "REFUSED"
	}
	return p.finishAgentReply(ctx, state, message, failure)
}

func (p *Processor) finishAgentReply(ctx context.Context, state *agentState, message, failure string) error {
	message = clean(message, 4000)
	if message == "" {
		message = "Richmod sudah memproses pesanmu."
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE source_event
		SET processing_status=CASE WHEN processing_status IN ('NEEDS_REVIEW','IGNORED') THEN processing_status ELSE 'PROCESSED' END,
		    parser_name='telegram-conversational-agent',parser_version='1'
		WHERE id=$1`, state.SourceEventID); err != nil {
		return err
	}
	if markup := agentPendingMarkup(state.History); markup != nil {
		err = enqueueReplyMarkup(ctx, tx, state.Update, message, markup)
	} else {
		err = enqueueReply(ctx, tx, state.Update, message)
	}
	if err != nil {
		return err
	}
	public := map[string]any{
		"agent_model_phases": state.ModelPhases,
		"agent_read_calls":   state.ReadCalls,
		"agent_side_effects": state.SideEffects,
	}
	if failure != "" {
		public["failure"] = failure
		public["nothing_recorded"] = state.SideEffects == 0
	}
	// The ASSISTANT turn commits with the reply, so memory never misses an answer
	// that was sent or records one that was not.
	if err := insertTurnTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "ASSISTANT", message, "", public); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// withoutTool drops one tool from a catalog.
func withoutTool(tools []gateway.ToolDefinition, name string) []gateway.ToolDefinition {
	out := make([]gateway.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != name {
			out = append(out, tool)
		}
	}
	return out
}
