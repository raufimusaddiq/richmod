package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

type prdAgentGateway struct{}

func (prdAgentGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func (prdAgentGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	for _, tool := range request.Tools {
		if tool.Name == "record_transaction" {
			args := json.RawMessage(`{"type":"EXPENSE","amount_idr":"5000","merchant":"Gorengan","category_slug":"food-drink","description":"jajan gorengan","note":null,"date_reference":"TODAY","explicit_date":null,"local_time":null,"ambiguous":false,"confidence":0.99,"category_confidence":0.99}`)
			return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "prd-expense", Name: "record_transaction", Arguments: args}}}, nil
		}
	}
	return gateway.AgentResponse{Text: "Recorded."}, nil
}

type ir04AgentGateway struct {
	calls     int
	category  string
	dateRef   string
	localTime string
	amount    string
	merchant  string
}

func (g *ir04AgentGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, errors.New("complex path must use the conversational agent")
}

func (g *ir04AgentGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.calls++
	for _, tool := range request.Tools {
		if tool.Name != "record_transaction" {
			continue
		}
		args := map[string]any{
			"type": "EXPENSE", "amount_idr": g.amount, "merchant": g.merchant,
			"category_slug": g.category, "description": g.merchant, "note": nil,
			"date_reference": g.dateRef, "explicit_date": nil, "local_time": g.localTime,
			"ambiguous": false, "confidence": 0.91, "category_confidence": 0.91,
		}
		encoded, _ := json.Marshal(args)
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "ir04-record", Name: "record_transaction", Arguments: encoded}}}, nil
	}
	return gateway.AgentResponse{}, errors.New("record_transaction was not exposed")
}

type ir04Judgment struct {
	calls        int
	questionSets []map[string]judgment.Question
	route        string
}

func (e *ir04Judgment) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	e.calls++
	e.questionSets = append(e.questionSets, request.Questions)
	answers := map[string]judgment.Answer{}
	if question, ok := request.Questions["route"]; ok {
		criteria, _ := question.Criteria.(map[string]any)
		route := e.route
		if route == "" {
			route = "NEEDS_GENERATIVE_AGENT"
		}
		answers["route"] = confidentChoice(criteria, route)
	}
	if question, ok := request.Questions["category"]; ok {
		criteria, _ := question.Criteria.(map[string]any)
		answers["category"] = confidentChoice(criteria, "food-drink")
	}
	return judgment.Result{Model: "ir04-test-jev", Answers: answers}, nil
}

type prdReviewGateway struct{ calls int }

func (*prdReviewGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func (g *prdReviewGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.calls++
	if g.calls == 1 {
		for _, tool := range request.Tools {
			if tool.Name == "resolve_review" {
				return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "prd-review", Name: "resolve_review", Arguments: json.RawMessage(`{"action":"CONFIRM","category_slug":"food-drink"}`)}}}, nil
			}
		}
	}
	return gateway.AgentResponse{Text: "Tinjauan selesai."}, nil
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

// prdFailingJudgment simulates a provider failure on the initial route call.
type prdFailingJudgment struct{}

func (prdFailingJudgment) Evaluate(context.Context, string, judgment.Request) (judgment.Result, error) {
	return judgment.Result{}, errors.New("judgment plane unavailable")
}

// PRD §8.3: when the initial Jev route call fails, a mutation request must fail
// closed. No mutation tool may be exposed to the generative agent, so no ledger
// row and no hidden model authority appear.
func TestIR04JudgmentRouteFailureExposesNoMutationAuthority(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "ir04-route-failure")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-drink')`, f.householdID)
	mustAgentTest(t, err)
	f.update.Message.MessageID = 79
	f.update.Message.Text = "makan siang 25rb hari ini"
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)

	agent := &ir04AgentGateway{amount: "25000", merchant: "Warung", category: "food-drink", dateRef: "TODAY", localTime: "12:30"}
	p := NewProcessor(f.pool, agent)
	p.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()) }
	p.SetJudgment(prdFailingJudgment{})
	if err = p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 0 {
		t.Fatalf("a failed Jev route must not fall through to the generative agent for a mutation, calls=%d", agent.calls)
	}
	var transactions int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&transactions))
	if transactions != 0 {
		t.Fatalf("a failed Jev route must not write the ledger, transactions=%d", transactions)
	}
}

type prdRouteJudgment struct{ route string }

func (e prdRouteJudgment) Evaluate(_ context.Context, _ string, _ judgment.Request) (judgment.Result, error) {
	return judgment.Result{Model: "test-jev", Answers: map[string]judgment.Answer{
		"route": confidentChoice(judgment.ChoiceCriteria(judgmentRouteCriteria), e.route),
	}}, nil
}

type prdUnclearReviewActionJudgment struct{}

func (prdUnclearReviewActionJudgment) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	criteria, _ := request.Questions["review_action"].Criteria.(map[string]any)
	return judgment.Result{Model: "test-jev", Answers: map[string]judgment.Answer{
		"review_action": confidentChoice(criteria, "OTHER_OR_UNCLEAR"),
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

func runIR04ComplexTransaction(t *testing.T, categorySlug string, wantJevCalls int, wantLane string, wantResidual []string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "ir04-single-pass")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food & Drink','food-drink')`, f.householdID)
	mustAgentTest(t, err)
	f.update.Message.MessageID = 77
	f.update.Message.Text = "I paid twenty five thousand for lunch today"
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)

	jev := &ir04Judgment{route: "NEEDS_GENERATIVE_AGENT"}
	agent := &ir04AgentGateway{amount: "25000", merchant: "Warung", category: categorySlug, dateRef: "TODAY", localTime: "12:30"}
	p := NewProcessor(f.pool, agent)
	p.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()) }
	p.SetJudgment(jev)
	p.SetTurnTelemetry(true)
	if err = p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	if jev.calls != wantJevCalls || agent.calls != 1 {
		t.Fatalf("IR-04 call budget: Jev=%d (want %d), generative=%d (want 1)", jev.calls, wantJevCalls, agent.calls)
	}
	if wantJevCalls == 2 {
		if len(jev.questionSets[1]) != 1 {
			t.Fatalf("residual Jev must get one question, got %v", jev.questionSets[1])
		}
		if _, ok := jev.questionSets[1]["category"]; !ok {
			t.Fatalf("residual Jev must ask category only, got %v", jev.questionSets[1])
		}
	}
	var status, gotCategory, lane string
	var residual []string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,c.slug FROM transaction t LEFT JOIN category c ON c.id=t.category_id JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, f.sourceID).Scan(&status, &gotCategory))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT lane,residual_dimensions FROM judgment_turn_telemetry WHERE source_event_id=$1`, f.sourceID).Scan(&lane, &residual))
	if status != "CONFIRMED" || gotCategory != "food-drink" {
		t.Fatalf("canonical transaction: status=%s category=%s", status, gotCategory)
	}
	if lane != wantLane || strings.Join(residual, ",") != strings.Join(wantResidual, ",") {
		t.Fatalf("turn telemetry: lane=%s residual=%v; want %s/%v", lane, residual, wantLane, wantResidual)
	}
}

func TestIR04ComplexClearExtractionSkipsSemanticReplay(t *testing.T) {
	runIR04ComplexTransaction(t, "food-drink", 1, string(judgmentLaneJevThenGenerative), nil)
}

func TestIR04CategoryResidualUsesOneCategoryOnlyRescue(t *testing.T) {
	runIR04ComplexTransaction(t, "", 2, string(judgmentLaneResidualJev), []string{"category"})
}

// PRD §8.1: a simple harvestable transaction is fully decided inside the one
// initial Jev route+bundle call. Any generative turn here is a call-budget bug.
func TestIR04SimpleHarvestableTransactionUsesNoGenerativeCall(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "ir04-simple-harvest")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-drink')`, f.householdID)
	mustAgentTest(t, err)
	f.update.Message.MessageID = 78
	f.update.Message.Text = "jajan gorengan 5k hari ini"
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)

	agent := &ir04AgentGateway{}
	p := NewProcessor(f.pool, agent)
	p.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()) }
	p.SetJudgment(simpleExpenseJudgment{})
	if err = p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 0 {
		t.Fatalf("harvestable transaction must not run the generative agent, calls=%d", agent.calls)
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1 AND t.amount=5000`, f.sourceID).Scan(&status))
	if status != "CONFIRMED" {
		t.Fatalf("harvestable transaction status=%s, want CONFIRMED", status)
	}
}

// PRD §24 T2: a real open review must survive a new expense in the same chat.
func TestPRDTelegramT2NewExpenseDoesNotResolveOpenReview(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "prd-t2-open-review")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-drink') RETURNING id`, f.householdID).Scan(&categoryID))
	reviewID, oldTransaction := createAgentTransactionReview(t, ctx, f, "70000", 101)
	f.update.Message.Text = "jajan gorengan 5k"
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)
	processor := NewProcessor(f.pool, prdAgentGateway{})
	processor.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()) }
	processor.SetJudgment(simpleExpenseJudgment{})
	if err := processor.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var confirmed, newExpenseNeedsReview, oldExpenseNeedsReview, reviewStatus, reviewRequestStatus int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE type='EXPENSE' AND status='CONFIRMED' AND amount=5000),count(*) FILTER(WHERE type='EXPENSE' AND status='NEEDS_REVIEW' AND amount=5000),count(*) FILTER(WHERE id=$2 AND status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, f.householdID, oldTransaction).Scan(&confirmed, &newExpenseNeedsReview, &oldExpenseNeedsReview))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item i JOIN review_request r ON r.review_item_id=i.id WHERE r.id=$1 AND i.status='OPEN'`, reviewID).Scan(&reviewStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE id=$1 AND status='OPEN'`, reviewID).Scan(&reviewRequestStatus))
	if confirmed != 0 || newExpenseNeedsReview != 1 || oldExpenseNeedsReview != 1 || reviewStatus != 1 || reviewRequestStatus != 1 {
		t.Fatalf("confirmed_new=%d new_expense_needs_review=%d existing_needs_review=%d review_item_open=%d request_open=%d", confirmed, newExpenseNeedsReview, oldExpenseNeedsReview, reviewStatus, reviewRequestStatus)
	}
}

// PRD §24 T3: an exact reply to a category review resolves that review only.
func TestPRDTelegramT3ExactCategoryReplyResolvesBoundReview(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "prd-t3-category-reply")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-drink') RETURNING id`, f.householdID).Scan(&categoryID))
	firstReview, firstTransaction := createAgentTransactionReview(t, ctx, f, "70000", 101)
	_, secondTransaction := createAgentTransactionReview(t, ctx, f, "90000", 202)
	f.update.Message.Text = "makanan"
	f.update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 101}
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)
	processor := NewProcessor(f.pool, &prdReviewGateway{})
	// A bounded action ruling must fall through for the exact free-form category.
	processor.SetJudgment(prdUnclearReviewActionJudgment{})
	if err := processor.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var firstStatus, secondStatus, category string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,category_id::text FROM transaction WHERE id=$1`, firstTransaction).Scan(&firstStatus, &category))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, secondTransaction).Scan(&secondStatus))
	var reviewStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT i.status FROM review_item i JOIN review_request r ON r.review_item_id=i.id WHERE r.id=$1`, firstReview).Scan(&reviewStatus))
	if firstStatus != "CONFIRMED" || category != categoryID || secondStatus != "NEEDS_REVIEW" || reviewStatus != "RESOLVED" {
		t.Fatalf("first=%s/%s second=%s review=%s", firstStatus, category, secondStatus, reviewStatus)
	}
}
