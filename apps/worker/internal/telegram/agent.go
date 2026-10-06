package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	var householdID, payloadText, processingStatus, sourceType string
	if err := p.pool.QueryRow(ctx, `
		SELECT s.household_id,p.payload_json::text,s.processing_status,s.source_type
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type IN ('TELEGRAM_TEXT','TELEGRAM_CALLBACK')`, sourceEventID).
		Scan(&householdID, &payloadText, &processingStatus, &sourceType); err != nil {
		return fmt.Errorf("load Telegram source event: %w", err)
	}
	if processingStatus == "PROCESSED" || processingStatus == "IGNORED" || processingStatus == "NEEDS_REVIEW" {
		return nil
	}
	if sourceType == "TELEGRAM_CALLBACK" {
		return p.Process(ctx, sourceEventID)
	}

	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil {
		return fmt.Errorf("decode Telegram source evidence: %w", err)
	}
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Pesan kosong diabaikan.")
	}
	if isHelpCommand(text) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, helpMessage)
	}
	// An exact reply to a proposal-keyed review (payslip pay date, missing amount) is
	// answered by the deterministic review lane, whether it replies to the card, to
	// the document's upload, or to a bound evidence notice. The agent has no
	// binding for those review kinds. Only exact replies take this lane: chat state
	// alone never owns a turn, and transaction-keyed reviews stay with the agent.
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
	stopTyping := p.startTyping(ctx, update.Message.Chat.ID)
	defer stopTyping()
	// The turn trace lives in the context so concurrent turns on the shared
	// Processor cannot contaminate each other's value telemetry.
	ctx, trace := withTurnTrace(ctx)
	trace.householdID = householdID
	trace.sourceEventID = sourceEventID
	generativeRan := false
	defer func() {
		// Turn-level Jev value: classify how this turn was resolved so
		// Jev-only, Jev-then-generative, and generative-only turns are countable.
		lane := judgmentLaneGenerativeOnly
		avoided := 0
		switch {
		case trace.consumed() && !generativeRan:
			lane = judgmentLaneJevOnly
			// Each bounded task replaced one would-be generative decision in the
			// lanes it now owns (route, transfer purpose, category, review action).
			avoided = len(trace.tasks)
		case trace.consumed():
			if len(trace.residualDimensions) > 0 {
				lane = judgmentLaneResidualJev
			} else {
				lane = judgmentLaneJevThenGenerative
			}
		}
		p.recordTurnTelemetry(context.WithoutCancel(ctx), householdID, sourceEventID, judgmentTurnObservation{
			Lane:                   lane,
			DecisionTasks:          trace.tasks,
			ResidualDimensions:     trace.residualDimensions,
			Model:                  trace.model,
			NativeToolCallsAvoided: avoided,
		})
	}()

	agentGateway, ok := p.gateway.(conversationalGateway)
	if !ok {
		return fmt.Errorf("conversational gateway unavailable")
	}
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
	var reviewBinding *agentReviewBinding
	var reviewPublic any
	reviewCount := 0
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
		reviewBinding, reviewPublic, reviewCount, err = p.loadAgentReviewBinding(ctx, householdID, update)
		if err != nil {
			return err
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
	freshEvidence := false
	// One candidates query per turn serves the context, the harvest decision and the
	// ledger guard. It is optional: a failure leaves no evidence context and no guard.
	recentCandidates, _ := p.recentEvidenceCandidates(ctx, householdID, update.Message.Chat.ID, update.Message.From.ID)
	freshDocuments := freshEvidenceDocuments(recentCandidates)
	if evidence == nil && !explicitReply && reviewBinding == nil && merchantBinding == nil &&
		!contextState.HasPendingAction && !contextState.HasPendingBatch && !contextState.HasSalaryChoice {
		evidence, recentEvidence = p.recentEvidenceContext(ctx, householdID, sourceEventID, update, recentCandidates)
		// Recent evidence is context only. It deliberately does not bind its open review:
		// a no-reply review binding is route-gated, and the route that would use it
		// (REVIEW_INTERACTION) is answered terminally by the fast path, so such a binding
		// would never reach the model. A review is resolved by an exact reply or by the existing deterministic reply lane.
		freshEvidence = (evidence != nil || len(recentEvidence) > 0) && len(freshDocuments) > 0
	}

	contextState.ActiveReview = reviewPublic
	contextState.ActiveReviewCount = reviewCount
	contextState.ReviewType, contextState.ReviewMode = "", ""
	if bound, ok := reviewPublic.(map[string]any); ok {
		contextState.ReviewType, _ = bound["review_type"].(string)
		contextState.ReviewMode, _ = bound["review_mode"].(string)
	}
	contextState.HasMerchantLearning = merchantBinding != nil && merchantCount == 1

	now := p.now().In(jakartaLocation())
	_ = p.persistTurn(ctx, householdID, sourceEventID, update, "USER", text, "", map[string]any{"current_jakarta_datetime": now.Format(time.RFC3339)})
	// The initial bounded bundle consumes the already-loaded category set and
	// pending-workflow state; the fast path must not re-query them.
	judgmentState := turnAgentContextState{
		Categories:          categories,
		HasPendingAction:    contextState.HasPendingAction,
		HasPendingBatch:     contextState.HasPendingBatch,
		HasSalaryChoice:     contextState.HasSalaryChoice,
		HasMerchantLearning: contextState.HasMerchantLearning,
		HasPendingWorkflow:  contextState.HasPendingAction || contextState.HasPendingBatch || contextState.HasSalaryChoice || contextState.HasMerchantLearning,
		HasRecentEvidence:   freshEvidence,
		ActiveReviewCount:   contextState.ActiveReviewCount,
		ExactReply:          explicitReply,
	}
	if handled, err := p.tryJudgmentFastPath(ctx, sourceEventID, householdID, update, text, now, &judgmentState); handled || err != nil {
		return err
	}

	generalTools := agentFinanceTools(
		categories,
		contextState.HasPendingAction,
		contextState.HasPendingBatch,
		contextState.ActiveReviewCount == 1,
		contextState.ReviewType,
		contextState.HasSalaryChoice,
		contextState.HasMerchantLearning,
		contextState.ReviewMode,
		p.judgmentPlaneConfigured,
	)
	// Jev owns bounded mutation authorization. A provider failure, undecided
	// route, or out-of-scope route leaves conversation and canonical READ tools
	// available but withholds every side effect. Exact review replies retain
	// their server-bound decision capability.
	if mutationAuthorityUnavailable(p.judgmentPlaneConfigured, judgmentState) {
		generalTools = readOnlyAgentTools(generalTools)
	}
	tools, workflowScope := applyAgentWorkflowToolPolicy(generalTools, update, reviewBinding, merchantBinding, judgmentState.Route)
	tools, workflowScope = applyEvidenceToolPolicy(generalTools, tools, workflowScope, evidence)

	turnContext := buildAgentTurnContext(text, now, categories, contextState)
	turnContext["workflow_scope"] = string(workflowScope)
	if mutationAuthorityUnavailable(p.judgmentPlaneConfigured, judgmentState) {
		turnContext["mutation_authority_unavailable"] = true
	}
	turnContext["merchant_learning_count"] = merchantCount
	if explicitReply && reviewBinding == nil && merchantBinding == nil && evidence == nil {
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
		Route:            judgmentState.Route,
		TurnContext:      turnContext,
		HasPendingAction: contextState.HasPendingAction,
		HasPendingBatch:  contextState.HasPendingBatch,
		HasSalaryChoice:  contextState.HasSalaryChoice,
		ReviewType:       contextState.ReviewType,
		ReviewMode:       contextState.ReviewMode,
		Analytics:        analyticscore.NewSession(p.pool, householdID, now),
		// Server-only: documents the user sent in the immediate window, for the
		// record_transaction duplicate guard. Never model-visible.
		FreshEvidenceDocuments: freshDocuments,
	}
	// An implicit binding is attached only when the route says this turn is that
	// interaction. Chat state alone never gets to own the turn (ADR-038
	// amendment). Exact bindings (pending action/batch/salary, explicit reply)
	// narrow unconditionally and so always keep their binding attached.
	if workflowScope == agentWorkflowUniqueReview || workflowScope == agentWorkflowExactReview {
		state.ReviewBinding = reviewBinding
		state.ReviewBindingCount = reviewCount
	}
	if workflowScope == agentWorkflowMerchantLearning || workflowScope == agentWorkflowExactMerchant {
		state.MerchantLearningBinding = merchantBinding
		state.MerchantLearningCount = merchantCount
	}
	if workflowScope == agentWorkflowPendingBatch {
		state.RequiredTool = "pending_batch_decision"
	}
	if handled, err := p.tryJudgmentBoundWorkflow(ctx, state, text); handled || err != nil {
		return err
	}

	turnCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.TotalTurnTimeout)
	defer cancel()
	generativeRan = true
	// A turn that needs several model phases can take a while; say so once
	// instead of leaving the household to wonder.
	stopProgress := startProgressNotice(ProgressNoticeDelay, func(noticeCtx context.Context) error {
		return p.enqueueProgressNotice(noticeCtx, state.Update)
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
	return func() { close(stop) }
}

func (p *Processor) runAgentLoop(ctx context.Context, model conversationalGateway, state *agentState) error {
	for state.ModelPhases < defaultAgentLimits.MaxModelPhases {
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
		phaseCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.ModelCallTimeout)
		response, err := model.AgentTurn(phaseCtx, state.SourceEventID, request)
		cancel()
		if err != nil {
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
		if state.RequiredTool != "" && (len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != state.RequiredTool) {
			return fmt.Errorf("required native tool %q was not selected", state.RequiredTool)
		}
		plan, err := validateAgentCallSet(response.ToolCalls, state, defaultAgentLimits)
		if err != nil {
			return p.finishAgentFailure(ctx, state, "Instruksi belum bisa diproses dengan aman. Coba jelaskan lagi dengan cara berbeda.")
		}

		switch plan.Class {
		case agentToolRead:
			results, err := p.executeAgentReadBatch(ctx, state, plan.Calls)
			if err != nil {
				return fmt.Errorf("conversational read batch: %w", err)
			}
			state.ReadCalls += len(results)
			state.History = append(state.History, results...)
			outputs := make([]gateway.AgentToolOutput, 0, len(results))
			for _, result := range results {
				public := agentToolResultPublic(result)
				outputs = append(outputs, gateway.AgentToolOutput{CallID: result.CallID, Output: public})
				_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, public)
			}
			state.PreviousResponseID = response.ResponseID
			state.PreviousToolCalls = append([]gateway.ToolCall(nil), response.ToolCalls...)
			state.PendingToolOutputs = outputs

		case agentToolSideEffect:
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
			if err != nil {
				return fmt.Errorf("conversational side effect %s: %w", call.Call.Name, err)
			}
			state.SideEffects++
			state.History = append(state.History, result)
			_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, agentToolResultPublic(result))
			if len(state.FreshEvidenceDocuments) > 0 {
				if _, err = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent',parser_version='1'
					WHERE id=ANY(SELECT source_event_id FROM document WHERE id=ANY($1::uuid[]) UNION SELECT source_event_id FROM document_page WHERE document_id=ANY($1::uuid[]))
					AND processing_status IN ('NEEDS_REVIEW','FAILED')`, state.FreshEvidenceDocuments); err != nil {
					return fmt.Errorf("finalize stale evidence source: %w", err)
				}
			}
		}
		if !synthesize {
				return p.finishAgentText(ctx, state, agentMutationFallback(result))
			}
			return p.synthesizeMutationResult(ctx, model, state, result)

		default:
			return p.finishAgentFailure(ctx, state, "Richmod tidak bisa menentukan aksi yang aman untuk pesan ini.")
		}
	}
	return p.finishAgentFailure(ctx, state, "Aku belum bisa menyelesaikan pertanyaan ini dalam satu percakapan. Coba persempit pertanyaannya.")
}

func validateAgentCallSet(calls []gateway.ToolCall, state *agentState, limits agentLimits) (agentCallPlan, error) {
	if len(calls) == 0 {
		return agentCallPlan{}, fmt.Errorf("empty tool set")
	}
	validated := make([]validatedAgentCall, 0, len(calls))
	readCount, sideEffectCount := 0, 0
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
			return agentCallPlan{}, fmt.Errorf("validate %s: %w", call.Name, err)
		}
		validated = append(validated, validatedAgentCall{Call: call, Class: class, Args: args})
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
		return agentCallPlan{Class: agentToolSideEffect, Calls: validated}, nil
	}
	if readCount != len(calls) {
		return agentCallPlan{}, fmt.Errorf("mixed tool classes")
	}
	if readCount > limits.MaxReadCallsPerResponse || state.ReadCalls+readCount > limits.MaxReadCallsPerTurn {
		return agentCallPlan{}, fmt.Errorf("read tool budget exceeded")
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

func (p *Processor) executeAgentReadBatch(ctx context.Context, state *agentState, calls []validatedAgentCall) ([]agentToolResult, error) {
	if state.Analytics == nil {
		state.Analytics = analyticscore.NewSession(p.pool, state.HouseholdID, state.Now)
	}
	results := make([]agentToolResult, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for index := range calls {
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
			return nil, err
		}
	}
	return results, nil
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
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "ASSISTANT", message, "", map[string]any{
		"agent_model_phases": state.ModelPhases,
		"agent_read_calls":   state.ReadCalls,
		"agent_side_effects": state.SideEffects,
	})
	return nil
}

func (p *Processor) finishAgentFailure(ctx context.Context, state *agentState, message string) error {
	return p.finishAgentText(ctx, state, message)
}

func agentMutationFallback(result agentToolResult) string {
	if result.Status == "RESIDUAL_FACTS_REQUIRED" {
		if missing, ok := result.Review["missing_fields"].([]string); ok {
			return reviewNeedsFactsMessage(missing)
		}
		return "Tinjauan ini masih memerlukan tanggal transaksi atau kategori yang valid."
	}
	if result.Status == "ALREADY_RECORDED_FROM_EVIDENCE" {
		// A refusal, not a recording: the user must be able to tell the two apart even
		// when the model call that would have worded it fails.
		return "Dokumen yang kamu kirim sudah tercatat, jadi tidak dicatat lagi. Ubah transaksi itu, atau sebutkan apa yang berbeda."
	}
	if result.Status == "INVALID_CANDIDATE" {
		// A refused merge, not a merge: said deterministically so a failed model call
		// cannot read as success.
		return "Itu bukan salah satu transaksi yang bisa digabung dengan struk ini, jadi belum ada yang diubah. Sebutkan transaksi yang dimaksud."
	}
	if result.Status == "DEFERRED" {
		return "Batch masih menunggu konfirmasi. Balas iya untuk mencatat, batal untuk membatalkan, atau sebutkan item yang ingin diubah."
	}
	if result.Mutation != nil {
		action, _ := result.Mutation["action"].(string)
		amount, _ := result.Mutation["amount_idr"].(string)
		switch action {
		case "TRANSACTION_RECORDED":
			if result.Status == "NEEDS_REVIEW" {
				if amount != "" {
					return "Transaksi Rp" + FormatIDR(amount) + " sudah masuk ke Kotak Tinjauan karena masih perlu konfirmasi."
				}
				return "Transaksi sudah masuk ke Kotak Tinjauan karena masih perlu konfirmasi."
			}
			if amount != "" {
				return "Transaksi Rp" + FormatIDR(amount) + " sudah tercatat."
			}
			return "Transaksi sudah tercatat."
		case "DUPLICATE_MERGED":
			return "Struk digabung dengan transaksi yang sudah ada, tidak ada transaksi baru."
		case "POSSIBLE_EXISTING_TRANSACTION":
			return "Saya menemukan transaksi serupa. Ingin mengubah transaksi yang sudah ada?"
		case "TRANSFER_RECORDED":
			if amount != "" {
				return "Transfer Rp" + FormatIDR(amount) + " sudah tercatat."
			}
			return "Transfer sudah tercatat."
		case "TRANSFER_ALREADY_RECORDED":
			return "Transfer ini sudah tercatat sebelumnya; tidak ada duplikasi baru."
		case "TRANSFER_RECONCILIATION_STAGED":
			return "Transfer ini perlu ditinjau karena ada transaksi yang mungkin sama."
		case "TRANSFER_RECONCILIATION_RESOLVED":
			return "Rekonsiliasi transfer sudah diselesaikan."
		case "TRANSFER_RECONCILIATION_IGNORED":
			return "Rekonsiliasi transfer sudah diabaikan tanpa mencatatnya sebagai transfer baru."
		case "BATCH_STAGED":
			return "Batch transaksi sudah disiapkan. Balas konfirmasi jika semuanya benar, atau sebutkan item yang ingin diubah."
		case "BATCH_UPDATED":
			return "Batch transaksi sudah diperbarui."
		case "BATCH_RECORDED":
			return "Semua transaksi dalam batch sudah tercatat."
		case "CORRECTION_STAGED", "CORRECT_EXISTING_TRANSACTION":
			return "Perubahan transaksi sudah disiapkan. Konfirmasi jika sudah benar."
		case "CORRECTION_RESOLVED":
			if confirmed, _ := result.Mutation["confirmed"].(bool); confirmed {
				return "Perubahan transaksi sudah disimpan."
			}
			return "Perubahan transaksi dibatalkan."
		case "SALARY_CHOICE_RESOLVED":
			choice, _ := result.Mutation["choice"].(string)
			switch choice {
			case "PRIMARY":
				return "Gaji utama sudah disimpan dan menjadi acuan siklus keuangan."
			case "ORDINARY":
				return "Gaji sudah disimpan sebagai pemasukan biasa."
			case "IGNORE":
				return "Gaji tersebut sudah diabaikan."
			}
		case "MERCHANT_LEARNING_RESOLVED":
			if remember, _ := result.Mutation["remember"].(bool); remember {
				return "Kategori merchant sudah disimpan sebagai aturan."
			}
			return "Kategori merchant tidak disimpan sebagai aturan."
		case "REVIEW_CONFIRMED", "REVIEW_TRANSFER_CLASSIFIED":
			return "Review sudah diselesaikan dan data keuangan diperbarui."
		case "REVIEW_IGNORED":
			return "Review sudah diselesaikan tanpa mencatatnya sebagai transaksi aktif."
		case "REVIEW_DETAIL_SAVED":
			if result.Status == "NEEDS_REVIEW" {
				if missing, ok := result.Review["missing_fields"].([]string); ok {
					return reviewNeedsFactsMessage(missing)
				}
			}
			return "Detail review sudah diperbarui."
		case "REVIEW_DETAIL_SAVED_AND_CONFIRMED":
			return "Detail review sudah diperbarui."
		case "WEALTH_ACCOUNT_SET":
			return "Akun kekayaan untuk observasi tersebut sudah diperbarui."
		case "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE":
			return "Observasi kekayaan sudah direklasifikasi sebagai pembelian aset."
		case "WEALTH_OBSERVATION_IGNORED":
			return "Observasi kekayaan sudah diabaikan."
		case "CYCLE_RESIDUAL_RESOLVED":
			return "Rekonsiliasi sisa siklus gaji sudah diselesaikan."
		case "CYCLE_RESIDUAL_REFRESHED":
			return "Nilai sisa siklus gaji berubah. Tinjau nilai terbaru sebelum menyelesaikannya."
		case "CYCLE_RESIDUAL_CLOSED":
			return "Rekonsiliasi sisa siklus gaji ditutup karena tidak lagi berlaku."
		case "ADD_MISSING_TRANSACTION_IN_TELEGRAM":
			return "Kirim transaksi yang belum tercatat sebagai pesan baru di sini (jangan balas kartu tinjauan). Setelah tersimpan, sisa siklus gaji dihitung ulang; tinjauan tetap terbuka jika masih perlu tindakan."
		}
	}

	switch result.Status {
	case "AMBIGUOUS_TARGET", "AMBIGUOUS_REVIEW":
		return "Ada lebih dari satu kandidat yang mungkin kamu maksud. Balas atau pilih item yang spesifik."
	case "TARGET_UNAVAILABLE":
		return "Referensi transaksi itu sudah tidak tersedia. Cari transaksinya lagi dulu."
	case "MISSING_TARGET":
		return "Transaksi mana yang ingin kamu ubah?"
	case "MISSING_CHANGE":
		return "Bagian mana dari transaksi itu yang ingin kamu ubah?"
	case "NO_PENDING_BATCH":
		return "Tidak ada batch transaksi aktif untuk dikonfirmasi."
	case "NO_PENDING_ACTION":
		return "Tidak ada perubahan transaksi aktif untuk dikonfirmasi."
	case "NO_PENDING_SALARY_CHOICE":
		return "Tidak ada pilihan gaji aktif yang perlu diselesaikan."
	case "NO_MERCHANT_LEARNING_PENDING":
		return "Tidak ada konfirmasi aturan merchant yang aktif."
	case "ACCOUNT_AMBIGUOUS":
		return "Rekening sumber belum bisa dikenali secara unik. Sebutkan nama rekening yang lebih spesifik."
	case "WEALTH_ACCOUNT_AMBIGUOUS", "MISSING_WEALTH_ACCOUNT":
		return "Akun kekayaan belum bisa dikenali secara unik. Sebutkan nama yang lebih spesifik."
	case "MISSING_REVIEW_DETAIL":
		return "Masih ada detail review yang perlu dilengkapi."
	case "MISSING_CATEGORY", "INVALID_CATEGORY":
		return "Kategori itu tidak ada di daftar keluarga ini. Pilih salah satu kategori pengeluaran yang tersedia."
	case "MISSING_BANK_FACTS":
		return "Nominal dan waktu transaksi masih perlu dilengkapi."
	case "INVALID_PAY_DATE":
		return "Tanggal pembayaran belum terbaca. Tulis tanggalnya, misalnya 25 Agu 2026."
	case "INVALID_TRANSACTION_DATE":
		return "Tanggal transaksi belum terbaca, jadi belum ada yang disimpan. Balas kartu tinjauan dengan tanggalnya, misalnya 2026-10-03."
	case "TRANSFER_RECONCILIATION_REQUIRED":
		return "Ada transaksi transfer yang mungkin sama. Detailnya perlu ditinjau sebelum observasi kekayaan bisa direklasifikasi."
	case "STALE_REVIEW_BINDING", "STALE_MERCHANT_LEARNING_BINDING":
		return "Target review sudah berubah atau selesai. Buka atau balas review terbaru sebelum melanjutkan."
	}
	return "Aksi keuangan sudah diproses."
}
