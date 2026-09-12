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
		WHERE s.id=$1 AND s.source_type IN ('TELEGRAM_TEXT','TELEGRAM_CALLBACK')`, sourceEventID).Scan(&householdID, &payloadText, &processingStatus, &sourceType); err != nil {
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
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Richmod belum bisa memproses percakapan ini. Coba lagi sebentar.")
	}
	categories, err := p.categorySlugs(ctx, householdID)
	if err != nil {
		return err
	}
	contextState, err := p.loadAgentContextState(ctx, householdID, sourceEventID, update)
	if err != nil {
		return err
	}
	now := p.now().In(jakartaLocation())
	_ = p.persistTurn(ctx, householdID, sourceEventID, update, "USER", text, "", map[string]any{"current_jakarta_datetime": now.Format(time.RFC3339)})

	tools := AgentFinanceTools(categories, contextState.HasPendingAction, contextState.HasPendingBatch, contextState.ActiveReviewCount == 1, contextState.ReviewType, contextState.HasSalaryChoice, contextState.HasMerchantLearning, contextState.ReviewMode)
	state := &agentState{
		SourceEventID: sourceEventID,
		HouseholdID:   householdID,
		Update:        update,
		Now:           now,
		Categories:    categories,
		Tools:         tools,
		TurnContext:   buildAgentTurnContext(text, now, categories, contextState),
	}

	turnCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.TotalTurnTimeout)
	defer cancel()
	return p.runAgentLoop(turnCtx, agentGateway, state)
}

func (p *Processor) runAgentLoop(ctx context.Context, model conversationalGateway, state *agentState) error {
	for state.ModelPhases < defaultAgentLimits.MaxModelPhases {
		request := gateway.AgentRequest{
			SystemPrompt:  conversationalAgentPrompt,
			Content:       agentModelContent(state),
			Tools:         state.Tools,
			AllowParallel: true,
		}
		phaseCtx, cancel := context.WithTimeout(ctx, defaultAgentLimits.PerModelCallTimeout)
		response, err := model.AgentTurn(phaseCtx, state.SourceEventID, request)
		cancel()
		if err != nil {
			return p.finishAgentFailure(ctx, state, "Richmod belum bisa memproses percakapan ini. Coba lagi sebentar.")
		}
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
				return p.finishAgentFailure(ctx, state, "Data keuangan belum bisa dibaca sekarang. Coba lagi sebentar.")
			}
			state.ReadCalls += len(results)
			state.History = append(state.History, results...)
			for _, result := range results {
				_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, agentToolResultPublic(result))
			}

		case agentToolSideEffect:
			call := plan.Calls[0]
			result, synthesize, err := p.executeAgentSideEffect(ctx, state, call.Call, call.Args, response.Metadata)
			if err != nil {
				return p.finishAgentFailure(ctx, state, "Aksi keuangan belum bisa diproses dengan aman. Coba lagi.")
			}
			state.SideEffects++
			state.History = append(state.History, result)
			_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, agentToolResultPublic(result))
			if !synthesize {
				return nil
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
	content := map[string]any{
		"instruction": "Write the final user-facing response for this completed finance action. Use only the authoritative result below. Do not add financial facts and do not request another action.",
		"current_user_text": state.TurnContext["current_user_text"],
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
		"turn_context":       state.TurnContext,
		"agent_tool_results": state.History,
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
		SET processing_status=CASE WHEN processing_status='NEEDS_REVIEW' THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END,
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
	_ = p.persistTurn(ctx, state.HouseholdID, state.SourceEventID, state.Update, "ASSISTANT", message, "", map[string]any{"agent_model_phases": state.ModelPhases, "agent_read_calls": state.ReadCalls, "agent_side_effects": state.SideEffects})
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
		}
	}
	switch result.Status {
	case "AMBIGUOUS_TARGET":
		return "Ada lebih dari satu transaksi yang mungkin kamu maksud. Sebutkan merchant, nominal, tanggal, atau pilih dari hasil pencarian sebelumnya."
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
	}
	return "Aksi keuangan sudah diproses."
}
