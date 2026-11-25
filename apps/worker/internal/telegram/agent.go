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

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
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
	stopTyping := p.startTyping(ctx, update.Message.Chat.ID)
	defer stopTyping()
	p.noteRepeatedInput(ctx, householdID, sourceEventID, update, receivedAt)
	resolved, err := p.resolveInteraction(ctx, householdID, sourceEventID, update, interactionOverride(parserName))
	if err != nil {
		return err
	}
	update = resolved.Update
	switch resolved.Kind {
	case interactionClosedCard:
		p.recordInteraction(ctx, householdID, sourceEventID, resolved.Card.ReviewItemID, interactionClosedReply, "")
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, closedCardReply)
	case interactionAmbiguous:
		p.recordInteraction(ctx, householdID, sourceEventID, "", interactionAmbiguousEvent, "")
		return p.askWhichCard(ctx, householdID, sourceEventID, update, resolved.Candidates, resolved.Answer)
	case interactionAnswerOrNew:
		return p.askAnswerOrNew(ctx, householdID, sourceEventID, update)
	case interactionCardStep:
		p.recordInteraction(ctx, householdID, sourceEventID, resolved.Card.ReviewItemID, interactionAnswered, resolved.Source)
		if resolved.ButtonData != "" {
			if handled, err := p.dispatchCallback(ctx, sourceEventID, householdID, syntheticTap(update, resolved.Card, resolved.ButtonData)); handled || err != nil {
				return err
			}
		}
	}
	if resolved.Unmatched {
		p.recordInteraction(ctx, householdID, sourceEventID, "", interactionUnmatched, "")
	}
	if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0 {
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

	explicitReply := update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0
	merchantBinding, merchantCount, err := p.loadAgentMerchantLearningBinding(ctx, householdID, update)
	if err != nil {
		return err
	}
	if !explicitReply && merchantBinding != nil && (!isYesNoAnswer(text) || contextState.HasPendingAction || contextState.HasPendingBatch) {
		merchantBinding, merchantCount = nil, 0
	}
	var reviewBinding *agentReviewBinding
	var reviewPublic any
	reviewCount := 0
	var openCards []map[string]any
	if explicitReply {
		if resolved.Kind == interactionCardStep && merchantBinding == nil && resolved.Card.Binding != nil {
			reviewBinding = resolved.Card.Binding
			reviewPublic, reviewCount = agentReviewBindingPublic(reviewBinding), 1
		}
	} else {
		for index := range resolved.OpenCards {
			openCards = append(openCards, agentReviewBindingPublic(&resolved.OpenCards[index]))
		}
		if resolved.Kind == interactionCardStep && resolved.Card.Binding != nil {
			reviewBinding = resolved.Card.Binding
			reviewPublic, reviewCount = agentReviewBindingPublic(reviewBinding), 1
		}
		if reviewBinding == nil {
			reviewCount = len(resolved.OpenCards)
		}
	}

	evidence, evidenceReview, evidenceErr := p.bindTurnEvidence(ctx, householdID, sourceEventID, update, reviewBinding, merchantBinding != nil, explicitReply)
	if evidenceErr != nil {
		slog.Default().Warn("bind turn evidence failed", "household_id", householdID, "source_event_id", sourceEventID, "error", evidenceErr)
		evidence, evidenceReview = nil, nil
	}
	if reviewBinding == nil && evidenceReview != nil {
		reviewBinding = evidenceReview
		reviewPublic, reviewCount = agentReviewBindingPublic(reviewBinding), 1
	}
	var recentEvidence []map[string]any
	recentCandidates, recentErr := p.recentEvidenceCandidates(ctx, householdID, update.Message.Chat.ID, update.Message.From.ID)
	if recentErr != nil {
		slog.Default().Warn("load recent evidence candidates failed", "household_id", householdID, "source_event_id", sourceEventID, "error", recentErr)
	}
	freshDocuments := freshEvidenceDocuments(recentCandidates)
	if evidence == nil && !explicitReply && reviewBinding == nil && merchantBinding == nil &&
		!contextState.HasPendingAction && !contextState.HasPendingBatch && !contextState.HasSalaryChoice {
		evidence, recentEvidence = p.recentEvidenceContext(ctx, householdID, sourceEventID, update, recentCandidates)
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
	if evidence == nil && len(recentEvidence) == 0 {
		generalTools = withoutTool(generalTools, "get_evidence_context")
	}
	tools, workflowScope := generalTools, agentWorkflowGeneral
	if explicitReply {
		tools, workflowScope = applyAgentWorkflowToolPolicy(generalTools, update, reviewBinding, merchantBinding)
		if workflowScope == agentWorkflowExplicitUnbound && evidence == nil {
			tools, workflowScope = generalTools, agentWorkflowGeneral
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
	if evidence != nil {
		turnContext["bound_evidence"] = evidence.Context
	}
	if len(recentEvidence) > 0 {
		turnContext["recent_evidence"] = recentEvidence
		turnContext["evidence_ambiguous"] = true
	}
	if merchantBinding != nil {
		turnContext["merchant_learning"] = map[string]any{"merchant": untrustedField(merchantBinding.Merchant), "category": untrustedField(merchantBinding.Category)}
	}
	state := &agentState{
		SourceEventID:          sourceEventID,
		HouseholdID:            householdID,
		Update:                 update,
		Now:                    now,
		Categories:             categories,
		Tools:                  tools,
		RequiredTool:           "",
		TurnContext:            turnContext,
		HasPendingAction:       contextState.HasPendingAction,
		HasPendingBatch:        contextState.HasPendingBatch,
		HasSalaryChoice:        contextState.HasSalaryChoice,
		ReviewType:             contextState.ReviewType,
		ReviewMode:             contextState.ReviewMode,
		Analytics:              analyticscore.NewSession(p.pool, householdID, now, sourceEventID),
		FreshEvidenceDocuments: freshDocuments,
		ReceivedAt:             receivedAt,
		activity:               new(atomic.Int32),
	}
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
		return p.deferForGateway(ctx, state, errConversationalGatewayMissing)
	}

	turnCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.TotalTurnTimeout)
	defer cancel()
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

const finalPhaseInstruction = `

FINAL PHASE: no more tools are available in this turn. Answer the user now using only the tool results you already have. If something could not be checked, say briefly what is missing; do not promise another lookup.`

const partialAnswerMessage = "Aku sudah mengumpulkan sebagian datanya, tapi belum sempat merangkum semuanya dalam satu jawaban. Coba tanyakan bagian yang paling penting dulu, ya."

func (p *Processor) runAgentLoop(ctx context.Context, model conversationalGateway, state *agentState) error {
	for state.ModelPhases < defaultAgentLimits.MaxModelPhases {
		finalPhase := state.ModelPhases == defaultAgentLimits.MaxModelPhases-1 && len(state.PendingToolOutputs) > 0
		request := gateway.AgentRequest{
			SystemPrompt:       conversationalAgentPrompt,
			Content:            agentModelContent(state),
			Tools:              state.Tools,
			AllowParallel:      true,
			PreviousResponseID: state.PreviousResponseID,
			PreviousToolCalls:  state.PreviousToolCalls,
			ToolOutputs:        state.PendingToolOutputs,
			ReadHistory:        append([]gateway.AgentReadPhase(nil), state.ReadHistory...),
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
				return p.deferForGateway(ctx, state, err)
			}
			if finalPhase && !IsGatewayUnavailable(err) && !errors.Is(err, context.DeadlineExceeded) {
				return p.finishAgentFailure(ctx, state, partialAnswerMessage, "PHASE_LIMIT")
			}
			return fmt.Errorf("%w: %w", errModelPhase, err)
		}
		if len(state.PendingToolOutputs) > 0 {
			state.ReadHistory = append(state.ReadHistory, gateway.AgentReadPhase{
				ToolCalls:   state.PreviousToolCalls,
				ToolOutputs: state.PendingToolOutputs,
			})
			if limit := defaultAgentLimits.MaxModelPhases - 1; limit > 0 && len(state.ReadHistory) > limit {
				state.ReadHistory = append([]gateway.AgentReadPhase(nil), state.ReadHistory[len(state.ReadHistory)-limit:]...)
			}
		}
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
			if !isNonMutatingRefusal(result) {
				state.SideEffects++
			}
			state.History = append(state.History, result)
			_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, agentToolResultPublic(result))
			if len(state.FreshEvidenceDocuments) > 0 {
				if err = p.settleFreshEvidenceSources(ctx, state); err != nil {
					return fmt.Errorf("finalize stale evidence source: %w", err)
				}
			}
			if state.ReplyQueued {
				return nil
			}
			if result.Status == "STALE_REVIEW_BINDING" {
				if answered := staleReviewReply(ctx, p.pool, state.HouseholdID, state.Update, ""); answered != "" {
					return p.finishAgentText(ctx, state, answered)
				}
			}
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

type agentArgumentsError struct {
	Call gateway.ToolCall
	Err  error
}

func (e *agentArgumentsError) Error() string {
	return fmt.Sprintf("validate %s: %v", e.Call.Name, e.Err)
}
func (e *agentArgumentsError) Unwrap() error { return e.Err }

const maxArgumentRepairs = 1

const maxRepairReasonBytes = 240

func repairableArguments(state *agentState, err error) (*agentArgumentsError, bool) {
	var rejected *agentArgumentsError
	if !errors.As(err, &rejected) || state.ArgumentRepairs >= maxArgumentRepairs || state.ModelPhases >= defaultAgentLimits.MaxModelPhases {
		return nil, false
	}
	return rejected, true
}

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
		args, err := validateAgentToolCall(call, state.ReviewType, state.ReviewMode)
		if err != nil {
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

func readCacheKey(name string, args map[string]any) string {
	encoded, _ := json.Marshal(args)
	return name + "|" + string(encoded)
}

func (p *Processor) synthesizeMutationResult(ctx context.Context, model conversationalGateway, state *agentState, result agentToolResult) error {
	if state.ModelPhases >= defaultAgentLimits.MaxModelPhases {
		return p.finishAgentText(ctx, state, agentMutationFallback(result))
	}
	instruction := "Write the final user-facing response for this completed finance action. Use only the authoritative result below. Do not add financial facts and do not request another action."
	if isNonMutatingRefusal(result) {
		instruction = "Nothing was recorded or changed: Go refused this finance action. Tell the user briefly that nothing was saved and what detail is missing or wrong, using only the authoritative result below. Do not claim the action was completed and do not request another action."
	}
	content := map[string]any{
		"instruction":                 instruction,
		"current_user_text":           state.TurnContext["current_user_text"],
		"authoritative_action_result": result,
	}
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
	if err := finalizeAgentSource(ctx, tx, state.HouseholdID, state.SourceEventID, "telegram-conversational-agent"); err != nil {
		return err
	}
	if reviewID := pendingMerchantLearningReviewID(state.History); reviewID != "" {
		err = enqueueMerchantLearningQuestion(ctx, tx, reviewID, state.Update, message, merchantLearningMarkup())
	} else if markup := agentPendingMarkup(state.History); markup != nil {
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
	if err := insertTurnTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "ASSISTANT", message, "", public); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func finalizeAgentSource(ctx context.Context, tx pgx.Tx, householdID, sourceEventID, parser string) error {
	if _, err := tx.Exec(ctx, `UPDATE source_event SET parser_name=$3,parser_version='1' WHERE id=$1 AND household_id=$2`, sourceEventID, householdID, parser); err != nil {
		return err
	}
	return reviewdomain.RefreshSourceLifecycle(ctx, tx, householdID, sourceEventID)
}

func (p *Processor) settleFreshEvidenceSources(ctx context.Context, state *agentState) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT s.id::text FROM source_event s WHERE s.household_id=$2 AND s.processing_status='NEEDS_REVIEW'
		AND s.id IN (SELECT source_event_id FROM document WHERE id=ANY($1::uuid[]) UNION SELECT source_event_id FROM document_page WHERE document_id=ANY($1::uuid[]))
		ORDER BY s.id`, state.FreshEvidenceDocuments, state.HouseholdID)
	if err != nil {
		return err
	}
	sources, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := finalizeAgentSource(ctx, tx, state.HouseholdID, source, "telegram-agent"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func withoutTool(tools []gateway.ToolDefinition, name string) []gateway.ToolDefinition {
	out := make([]gateway.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != name {
			out = append(out, tool)
		}
	}
	return out
}
