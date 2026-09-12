package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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
	if strings.HasPrefix(strings.ToLower(text), "/start") {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, "Kirim transaksi atau tanyakan kondisi keuangan rumah tangga. Contoh: makan siang 50rb, atau bulan ini lebih boros nggak?")
	}

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

	tools := AgentFinanceTools(
		categories,
		contextState.HasPendingAction,
		contextState.HasPendingBatch,
		contextState.ActiveReviewCount == 1,
		contextState.ReviewType,
		contextState.HasSalaryChoice,
		contextState.HasMerchantLearning,
		contextState.ReviewMode,
	)
	tools, workflowScope := applyAgentWorkflowToolPolicy(tools, update, reviewBinding, merchantBinding)

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
		TurnContext:             turnContext,
		ReviewBinding:           reviewBinding,
		ReviewBindingCount:      reviewCount,
		MerchantLearningBinding: merchantBinding,
		MerchantLearningCount:   merchantCount,
	}

	turnCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.TotalTurnTimeout)
	defer cancel()
	return p.runAgentLoop(turnCtx, agentGateway, state)
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
		state.PreviousToolCalls = nil
		state.PendingToolOutputs = nil
		state.ModelPhases++

		if len(response.ToolCalls) == 0 {
			return p.finishAgentText(ctx, state, response.Text)
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
	phaseCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.PerModelCallTimeout)
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
	if err := enqueueReply(ctx, tx, state.Update, message); err != nil {
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
	if result.Mutation != nil {
		action, _ := result.Mutation["action"].(string)
		amount, _ := result.Mutation["amount_idr"].(string)
		switch action {
		case "TRANSACTION_RECORDED":
			if result.Status == "NEEDS_REVIEW" {
				if amount != "" {
					return "Transaksi Rp" + FormatIDR(amount) + " sudah masuk ke Review karena masih perlu konfirmasi."
				}
				return "Transaksi sudah masuk ke Review karena masih perlu konfirmasi."
			}
			if amount != "" {
				return "Transaksi Rp" + FormatIDR(amount) + " sudah tercatat."
			}
			return "Transaksi sudah tercatat."
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
		case "REVIEW_DETAIL_SAVED", "REVIEW_DETAIL_SAVED_AND_CONFIRMED":
			return "Detail review sudah diperbarui."
		case "WEALTH_ACCOUNT_SET":
			return "Wealth Account untuk observasi tersebut sudah diperbarui."
		case "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE":
			return "Observasi Wealth sudah direklasifikasi sebagai pembelian aset."
		case "WEALTH_OBSERVATION_IGNORED":
			return "Observasi Wealth sudah diabaikan."
		case "PREPARE_WEALTH_SNAPSHOT":
			return "Wealth Account sudah siap; lanjutkan snapshot lengkap di halaman Wealth."
		case "CYCLE_RESIDUAL_RESOLVED":
			return "Rekonsiliasi sisa salary cycle sudah diselesaikan."
		case "CYCLE_RESIDUAL_REFRESHED":
			return "Nilai sisa salary cycle berubah. Tinjau nilai terbaru sebelum menyelesaikannya."
		case "CYCLE_RESIDUAL_CLOSED":
			return "Rekonsiliasi sisa salary cycle ditutup karena tidak lagi berlaku."
		case "ADD_MISSING_TRANSACTION_IN_WEB":
			return "Tambahkan transaksi yang belum ada lewat Review Inbox di web, lalu lanjutkan rekonsiliasinya."
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
		return "Wealth Account belum bisa dikenali secara unik. Sebutkan nama yang lebih spesifik."
	case "MISSING_REVIEW_DETAIL":
		return "Masih ada detail review yang perlu dilengkapi."
	case "MISSING_CATEGORY", "INVALID_CATEGORY":
		return "Kategori belum valid. Pilih kategori pengeluaran yang tersedia."
	case "MISSING_BANK_FACTS":
		return "Nominal dan waktu transaksi masih perlu dilengkapi."
	case "INVALID_PAY_DATE":
		return "Tanggal pembayaran belum valid."
	case "TRANSFER_RECONCILIATION_REQUIRED":
		return "Ada transaksi transfer yang mungkin sama. Detailnya perlu ditinjau sebelum observasi Wealth bisa direklasifikasi."
	case "STALE_REVIEW_BINDING", "STALE_MERCHANT_LEARNING_BINDING":
		return "Target review sudah berubah atau selesai. Buka atau balas review terbaru sebelum melanjutkan."
	}
	return "Aksi keuangan sudah diproses."
}
