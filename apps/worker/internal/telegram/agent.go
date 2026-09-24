		reviewBinding, reviewPublic, reviewCount, err = p.loadAgentReviewBinding(ctx, householdID, update)
		if err != nil {
			return err
		}
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
	// pending-workflow state; the fast path must not re-query them (PRD §11).
	judgmentState := turnAgentContextState{
		Categories:          categories,
		HasPendingAction:    contextState.HasPendingAction,
		HasPendingBatch:     contextState.HasPendingBatch,
		HasSalaryChoice:     contextState.HasSalaryChoice,
		HasMerchantLearning: contextState.HasMerchantLearning,
		HasPendingWorkflow:  contextState.HasPendingAction || contextState.HasPendingBatch || contextState.HasSalaryChoice || contextState.HasMerchantLearning,
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
	tools, workflowScope := applyAgentWorkflowToolPolicy(generalTools, update, reviewBinding, merchantBinding, judgmentState.Route)

	turnContext := buildAgentTurnContext(text, now, categories, contextState)
	turnContext["workflow_scope"] = string(workflowScope)
	turnContext["merchant_learning_count"] = merchantCount
	if explicitReply && reviewBinding == nil && merchantBinding == nil {
		turnContext["explicit_reply_unbound"] = true
	}
	if merchantBinding != nil {
		turnContext["merchant_learning"] = map[string]any{"merchant": merchantBinding.Merchant, "category": merchantBinding.Category}
	}
	state := &agentState{
		SourceEventID:           sourceEventID,
		HouseholdID:             householdID,
		Update:                  update,
		Now:                     now,
		Categories:              categories,
		Tools:                   tools,
		RequiredTool:            "",
		TurnContext:             turnContext,
		HasPendingAction:        contextState.HasPendingAction,
		HasPendingBatch:         contextState.HasPendingBatch,
		HasSalaryChoice:         contextState.HasSalaryChoice,
		ReviewMode:              contextState.ReviewMode,
		JudgmentRoute:           judgmentState.Route,
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
		phaseCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.PerModelCallTimeout)
		response, err := model.AgentTurn(phaseCtx, state.SourceEventID, request)
		cancel()
		if err != nil {
			return fmt.Errorf("conversational model phase: %w", err)
		}
		// Continuation data is single-use. If this response asks for another READ
		// phase, the new response/call IDs replace it below.
		state.PreviousResponseID = ""