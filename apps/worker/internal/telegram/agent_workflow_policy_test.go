package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// eagerEngine answers every question it is asked with the first allowed option
// (or an affirmative Noul), so a workflow test does not have to guess the exact
// question keys the processor sends.
type eagerEngine struct{}

func (eagerEngine) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	answers := map[string]judgment.Answer{}
	for key, question := range request.Questions {
		criteria, ok := question.Criteria.(map[string]any)
		if !ok {
			answers[key] = decidedNoul(0.99)
			continue
		}
		labels := judgment.CriteriaLabels(criteria)
		if len(labels) == 0 {
			continue
		}
		// Prefer an affirmative label when the workflow offers one.
		choice := labels[0]
		for _, preferred := range []string{"CONFIRM", "PRIMARY", "EXPENSE"} {
			for _, label := range labels {
				if label == preferred {
					choice = preferred
				}
			}
		}
		answers[key] = confidentChoice(criteria, choice)
	}
	return judgment.Result{Model: "stub-jev", Answers: answers}, nil
}

func sideEffectNames(tools []gateway.ToolDefinition) map[string]bool {
	out := map[string]bool{}
	for _, tool := range tools {
		if class, ok := agentToolClassFor(tool.Name); ok && class == agentToolSideEffect {
			out[tool.Name] = true
		}
	}
	return out
}

func TestExplicitReviewReplyExposesOnlyBoundReviewMutation(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 77}
	tools := AgentFinanceTools([]string{"dining"}, true, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, nil)
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowExactReview {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 1 || !writes["resolve_review"] {
		t.Fatalf("writes=%v; want resolve_review only", writes)
	}
}

func TestExplicitMerchantReplyExposesOnlyMerchantMutation(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 88}
	tools := AgentFinanceTools([]string{"dining"}, true, true, false, "", true, true, "")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, nil, &agentMerchantLearningBinding{ReviewRequestID: "review", TransactionID: "tx", TelegramMessageID: 88})
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowExactMerchant {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 1 || !writes["resolve_merchant_learning"] {
		t.Fatalf("writes=%v; want resolve_merchant_learning only", writes)
	}
}

func TestStaleExplicitReplyExposesNoSideEffects(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 99}
	tools := AgentFinanceTools([]string{"dining"}, true, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, nil, nil)
	if scope != agentWorkflowExplicitUnbound {
		t.Fatalf("scope=%s", scope)
	}
	if writes := sideEffectNames(filtered); len(writes) != 0 {
		t.Fatalf("stale explicit reply exposed side effects: %v", writes)
	}
}

func TestPendingCorrectionOutranksOtherImplicitWrites(t *testing.T) {
	var update telegramUpdate
	tools := AgentFinanceTools([]string{"dining"}, true, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, &agentMerchantLearningBinding{ReviewRequestID: "merchant-review", TransactionID: "tx"})
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowPendingAction {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 2 || !writes["confirm_pending_action"] || !writes["cancel_pending_action"] {
		t.Fatalf("writes=%v; want pending correction writes only", writes)
	}
}

func TestPendingBatchOutranksUniqueReview(t *testing.T) {
	var update telegramUpdate
	tools := AgentFinanceTools([]string{"dining"}, false, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, nil)
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowPendingBatch {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 1 || !writes["pending_batch_decision"] {
		t.Fatalf("writes=%v; want pending batch writes only", writes)
	}
}

func TestUniqueReviewOutranksGeneralWrites(t *testing.T) {
	var update telegramUpdate
	tools := AgentFinanceTools([]string{"dining"}, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, nil)
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowUniqueReview {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 1 || !writes["resolve_review"] {
		t.Fatalf("writes=%v; want resolve_review only", writes)
	}
}

func TestCycleResidualToolUsesWealthHintsNotUUIDs(t *testing.T) {
	tools := AgentFinanceTools(nil, false, false, true, "CYCLE_RESIDUAL_ALLOCATION", false, false, "CYCLE_RESIDUAL")
	var resolve *gateway.ToolDefinition
	for i := range tools {
		if tools[i].Name == "resolve_review" {
			resolve = &tools[i]
			break
		}
	}
	if resolve == nil {
		t.Fatal("resolve_review missing for cycle residual")
	}
	properties, _ := resolve.Parameters["properties"].(map[string]any)
	allocations, _ := properties["allocations"].(map[string]any)
	items, _ := allocations["items"].(map[string]any)
	itemProperties, _ := items["properties"].(map[string]any)
	if _, ok := itemProperties["wealth_account_hint"]; !ok {
		t.Fatalf("cycle residual allocation missing wealth_account_hint: %#v", itemProperties)
	}
	if _, ok := itemProperties["wealth_account_id"]; ok {
		t.Fatalf("cycle residual exposed canonical wealth_account_id: %#v", itemProperties)
	}
}

// PRD §26 / completion criterion 15: the bounded generative tools for
// server-owned workflows are fallback-only. When the judgment plane is
// configured, tryJudgmentBoundWorkflow consumes these turns before the
// generative loop runs, so the generative tool definition is never reached.
// This test pins that pre-emption so a future change cannot silently reopen a
// second mutation authority for the same workflow.
func TestJevOwnedWorkflowsPreemptTheirGenerativeTools(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "jev-preempt-salary")
	// A pending payslip is a server-owned workflow: the bounded plane owns the
	// turn, so the generative resolve_salary_choice tool is never offered a chance
	// to mutate and the turn must resolve on the judgment path.
	var transactionID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'INCOME','CONFIRMED',16000000,'IDR',now(),'Acme',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	_, err := f.pool.Exec(ctx, `INSERT INTO salary_pending_choice(household_id,telegram_user_id,telegram_chat_id,transaction_id,employer,payroll_period,pay_date,status) VALUES($1,$2,$3,$4,'Acme','2026-09-01','2026-09-01','PENDING')`, f.householdID, f.chatID, f.chatID, transactionID)
	mustAgentTest(t, err)

	processor := NewProcessor(f.pool, nil)
	processor.SetJudgment(eagerEngine{})
	state := &agentState{HouseholdID: f.householdID, SourceEventID: f.sourceID, Update: f.update, HasSalaryChoice: true}
	handled, err := processor.tryJudgmentBoundWorkflow(ctx, state, "gaji pokok")
	mustAgentTest(t, err)
	if !handled {
		t.Fatal("the bounded salary workflow must consume the turn before the generative loop")
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM salary_pending_choice WHERE transaction_id=$1`, transactionID).Scan(&status))
	if status == "PENDING" {
		t.Fatal("the bounded workflow claimed the turn but did not resolve the pending payslip")
	}
}
