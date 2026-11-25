package telegram

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type callingGateway struct {
	*scriptedPicker
	call     gateway.ToolCall
	requests []gateway.AgentRequest
}

func (g *callingGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.agentCalls++
	g.requests = append(g.requests, request)
	if len(request.ToolOutputs) == 0 {
		return gateway.AgentResponse{ResponseID: "call", ToolCalls: []gateway.ToolCall{g.call}}, nil
	}
	return gateway.AgentResponse{Text: "Belum bisa kujawab dari sini."}, nil
}

func (g *callingGateway) offered(name string) bool {
	return len(g.requests) > 0 && slices.ContainsFunc(g.requests[0].Tools, func(tool gateway.ToolDefinition) bool { return tool.Name == name })
}

func (h *contractHarness) answerer(name string, arguments string) *callingGateway {
	gw := &callingGateway{scriptedPicker: h.picker, call: gateway.ToolCall{CallID: "answer", Name: name, Arguments: json.RawMessage(arguments)}}
	h.p = NewProcessor(h.f.pool, gw)
	return gw
}

func TestAssistantNeverAnswersACard(t *testing.T) {
	runContractRows(t, []contractRow{
		{"a typed correction on a card cannot be confirmed by an assistant tool call", fixKind, nil, func(h *contractHarness) {
			gw := h.answerer("resolve_review", `{"action":"CONFIRM","candidate_ref":null,"source_account_hint":null,"wealth_account_hint":null,"category_slug":null,"merchant":null,"description":null,"pay_date":null,"amount_idr":null,"transaction_at":null,"allocations":[]}`)
			messageID, _, _ := h.current()
			h.question(messageID, "makan siang tim")
			if gw.agentCalls == 0 || gw.offered("resolve_review") {
				t.Fatalf("the assistant did not run, or was offered resolve_review (calls=%d)", gw.agentCalls)
			}
			h.wantItemActive()
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"a typed correction on a card cannot be ignored by an assistant tool call", fixKind, nil, func(h *contractHarness) {
			h.answerer("resolve_review", `{"action":"IGNORE","candidate_ref":null,"source_account_hint":null,"wealth_account_hint":null,"category_slug":null,"merchant":null,"description":null,"pay_date":null,"amount_idr":null,"transaction_at":null,"allocations":[]}`)
			messageID, _, _ := h.current()
			h.question(messageID, "makan siang tim")
			h.wantItemActive()
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"a typed email-facts reply cannot be ignored by an assistant tool call", factsEmail, nil, func(h *contractHarness) {
			h.answerer("resolve_review", `{"action":"IGNORE","candidate_ref":null,"source_account_hint":null,"wealth_account_hint":null,"category_slug":null,"merchant":null,"description":null,"pay_date":null,"amount_idr":null,"transaction_at":null,"allocations":[]}`)
			messageID, _, _ := h.current()
			h.question(messageID, "kayaknya ini email lama deh")
			h.wantItemActive()
		}},
		{"the word abaikan typed as a reply finishes the card through the engine without the assistant", factsEmail, nil, func(h *contractHarness) {
			h.reply("abaikan")
			h.wantAssistantCalls(0)
			h.wantNoButtons()
		}},
		{"the word abaikan typed as a plain message finishes the single waiting card without the assistant", factsEmail, nil, func(h *contractHarness) {
			h.plain("abaikan")
			h.wantAssistantCalls(0)
			h.wantNoButtons()
		}},
	})
}

func TestAssistantCannotResolveAPayslipChoice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "no-salary-tool")
	var transactionID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'INCOME','CONFIRMED',8500000,'IDR',now(),'PT Contoh',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	_, err := f.pool.Exec(ctx, `INSERT INTO salary_pending_choice(household_id,telegram_user_id,telegram_chat_id,transaction_id,employer,payroll_period,pay_date,status) VALUES($1,$2,$3,$4,'PT Contoh','2026-09-01','2026-09-01','PENDING')`, f.householdID, f.chatID, f.chatID, transactionID)
	mustAgentTest(t, err)
	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "salary", Name: "resolve_salary_choice", Arguments: json.RawMessage(`{"choice":"PRIMARY"}`)}}}
		}
		return gateway.AgentResponse{Text: "Belum bisa kujawab dari sini."}
	}}
	_, err = runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "gaji utama ya", 0)
	mustAgentTest(t, err)
	if model.toolNames(0)["resolve_salary_choice"] {
		t.Fatal("the assistant was offered the salary choice tool")
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM salary_pending_choice WHERE transaction_id=$1`, transactionID).Scan(&status))
	if status != "PENDING" || countRows(t, ctx, f, `SELECT count(*) FROM salary_source WHERE household_id=$1`, f.householdID) != 0 {
		t.Fatalf("the assistant answered the payslip: choice=%s", status)
	}
}

func TestReplyToTheMerchantLearningQuestionGoesToTheEngineNotTheAssistant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "learning-reply")
	reviewID, _ := createMerchantLearningReview(t, ctx, f, 301, "merchantx")
	_, err := f.pool.Exec(ctx, `UPDATE review_request_recipient SET merchant_learning_message_id=302 WHERE review_request_id=$1`, reviewID)
	mustAgentTest(t, err)
	model := &callingGateway{scriptedPicker: &scriptedPicker{}, call: gateway.ToolCall{CallID: "learn", Name: "resolve_merchant_learning", Arguments: json.RawMessage(`{"remember":true}`)}}
	_, err = sendText(t, ctx, f, NewProcessor(f.pool, model), "ya", 302)
	mustAgentTest(t, err)
	if model.agentCalls != 0 {
		t.Fatalf("the assistant was asked to answer the learning question (%d calls)", model.agentCalls)
	}
	var autoApply bool
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(bool_or(auto_apply),false) FROM merchant_alias WHERE household_id=$1`, f.householdID).Scan(&autoApply))
	if !autoApply {
		t.Fatal("the user's own yes did not save the merchant rule")
	}
}
