package telegram

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

type prdAgentGateway struct{}

func (prdAgentGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func (prdAgentGateway) AgentTurn(context.Context, string, gateway.AgentRequest) (gateway.AgentResponse, error) {
	return gateway.AgentResponse{Text: "Recorded."}, nil
}

type simpleExpenseJudgment struct{}

func (simpleExpenseJudgment) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	answers := map[string]judgment.Answer{}
	for key, question := range request.Questions {
		switch key {
		case "route":
			criteria, _ := question.Criteria.(map[string]any)
			answers[key] = confidentChoice(criteria, "CREATE_TRANSACTION")
		case "transaction_type":
			answers[key] = confidentChoice(judgmentTypeCriteria, "EXPENSE")
		case "category":
			criteria, _ := question.Criteria.(map[string]any)
			answers[key] = confidentChoice(criteria, "food-drink")
		case "amount_support", "date_support":
			answers[key] = decidedNoul(0.99)
		case "material_ambiguity":
			answers[key] = decidedNoul(0.02)
		}
	}
	return judgment.Result{Model: "test-jev", Answers: answers}, nil
}

type prdRouteJudgment struct{ route string }

func (e prdRouteJudgment) Evaluate(_ context.Context, _ string, _ judgment.Request) (judgment.Result, error) {
	return judgment.Result{Model: "test-jev", Answers: map[string]judgment.Answer{
		"route": confidentChoice(judgment.ChoiceCriteria(judgmentRouteCriteria), e.route),
	}}, nil
}

func TestPRDTelegramT4ToT6RoutesReachTheirAgentTools(t *testing.T) {
	for _, test := range []struct {
		route string
		want  []string
	}{
		{"CREATE_TRANSFER", []string{"record_transfer"}},
		{"SEARCH_TRANSACTIONS", []string{"search_transactions"}},
		{"CORRECT_TRANSACTION", []string{"search_transactions", "propose_transaction_correction"}},
	} {
		t.Run(test.route, func(t *testing.T) {
			p := &Processor{}
			p.SetJudgment(prdRouteJudgment{route: test.route})
			state := &turnAgentContextState{}
			handled, err := p.tryJudgmentFastPath(context.Background(), "source", "household", telegramUpdate{}, "look up", time.Now(), state)
			if err != nil || handled {
				t.Fatalf("route=%s handled=%t err=%v; agent routes must fall through", test.route, handled, err)
			}
			if state.Route != test.route {
				t.Fatalf("route=%q, want %q", state.Route, test.route)
			}
			tools, scope := applyAgentWorkflowToolPolicy(agentFinanceTools(nil, false, false, false, "", false, false, "", true), telegramUpdate{}, nil, nil, state.Route)
			if scope != agentWorkflowGeneral {
				t.Fatalf("scope=%s, want general agent workflow", scope)
			}
			for _, name := range test.want {
				if !agentToolAvailable(tools, name) {
					t.Errorf("route %s omitted agent tool %s", test.route, name)
				}
			}
			for _, name := range []string{"ask_clarification", "finance_help", "finance_out_of_scope"} {
				if agentToolAvailable(tools, name) {
					t.Errorf("route %s exposed terminal tool %s", test.route, name)
				}
			}
		})
	}
}

func TestPRDTelegramT1SimpleExpenseConfirmsWithoutReviewOrTypedFields(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()

	f := newAgentIntegrationFixture(t, "prd-t1-simple-expense")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-drink') RETURNING id`, f.householdID).Scan(&categoryID))
	if _, err := f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,jsonb_build_object('message',jsonb_build_object('message_id',1,'text','jajan gorengan 5k','from',jsonb_build_object('id',$2::bigint),'chat',jsonb_build_object('id',$2::bigint))))`, f.sourceID, f.chatID); err != nil {
		t.Fatal(err)
	}
	p := NewProcessor(f.pool, prdAgentGateway{})
	p.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()) }
	p.SetJudgment(simpleExpenseJudgment{})
	if err := p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var status, gotCategory, sourceStatus string
	var reviews int
	if err := f.pool.QueryRow(ctx, `SELECT t.status,t.category_id::text,s.processing_status,(SELECT count(*) FROM review_request WHERE transaction_id=t.id) FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id JOIN source_event s ON s.id=e.source_event_id WHERE t.household_id=$1 AND t.amount=5000 AND t.type='EXPENSE'`, f.householdID).Scan(&status, &gotCategory, &sourceStatus, &reviews); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || gotCategory != categoryID || sourceStatus != "PROCESSED" || reviews != 0 {
		t.Fatalf("status=%s category=%s source=%s reviews=%d", status, gotCategory, sourceStatus, reviews)
	}
}
