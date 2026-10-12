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
)

type prdAgentGateway struct{}

func (prdAgentGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func (prdAgentGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	for _, tool := range request.Tools {
		if tool.Name == "record_transaction" {
			args := json.RawMessage(`{"type":"EXPENSE","amount_idr":"5000","merchant":"Gorengan","category_slug":"food-drink","description":"jajan gorengan","note":null,"date_reference":"TODAY","date_provenance":"USER_STATED","explicit_date":null,"local_time":null,"ambiguous":false,"confidence":0.99,"category_confidence":0.99}`)
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
			"date_reference": g.dateRef, "date_provenance": "USER_STATED", "explicit_date": nil, "local_time": g.localTime,
			"ambiguous": false, "confidence": 0.91, "category_confidence": 0.91,
		}
		encoded, _ := json.Marshal(args)
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "ir04-record", Name: "record_transaction", Arguments: encoded}}}, nil
	}
	return gateway.AgentResponse{}, errors.New("record_transaction was not exposed")
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

func TestTelegramOrdinaryTurnExposesFullAgentCatalog(t *testing.T) {
	tools, scope := applyAgentWorkflowToolPolicy(agentFinanceTools(nil, false, false, false, "", false, false, ""), telegramUpdate{}, nil, nil)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s, want general agent workflow", scope)
	}
	for _, name := range []string{"record_transaction", "record_transfer", "search_transactions", "propose_transaction_correction"} {
		if !agentToolAvailable(tools, name) {
			t.Errorf("ordinary turn omitted agent tool %s", name)
		}
	}
	for _, name := range []string{"ask_clarification", "finance_help", "finance_out_of_scope"} {
		if agentToolAvailable(tools, name) {
			t.Errorf("ordinary turn exposed terminal tool %s", name)
		}
	}
}

func TestTelegramT1SimpleExpenseConfirmsWithoutReviewOrTypedFields(t *testing.T) {
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
	if err := p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var status, gotCategory, sourceStatus, description string
	var reviews int
	if err := f.pool.QueryRow(ctx, `SELECT t.status,t.category_id::text,s.processing_status,COALESCE(t.description,''),(SELECT count(*) FROM review_request WHERE transaction_id=t.id) FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id JOIN source_event s ON s.id=e.source_event_id WHERE t.household_id=$1 AND t.amount=5000 AND t.type='EXPENSE'`, f.householdID).Scan(&status, &gotCategory, &sourceStatus, &description, &reviews); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || gotCategory != categoryID || sourceStatus != "PROCESSED" || reviews != 0 || description == "" {
		t.Fatalf("status=%s category=%s source=%s reviews=%d description=%q", status, gotCategory, sourceStatus, reviews, description)
	}
}

func runIR04ComplexTransaction(t *testing.T, categorySlug, wantStatus string, wantResidual []string) {
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

	agent := &ir04AgentGateway{amount: "25000", merchant: "Warung", category: categorySlug, dateRef: "TODAY", localTime: "12:30"}
	p := NewProcessor(f.pool, agent)
	p.now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()) }
	if err = p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 {
		t.Fatalf("call budget: generative=%d (want 1)", agent.calls)
	}
	var status, gotCategory, sourceStatus, reviewType string
	var residual []string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(c.slug,''),s.processing_status,COALESCE((SELECT r.review_type FROM review_request r WHERE r.transaction_id=t.id),'') FROM transaction t LEFT JOIN category c ON c.id=t.category_id JOIN transaction_evidence e ON e.transaction_id=t.id JOIN source_event s ON s.id=e.source_event_id WHERE e.source_event_id=$1`, f.sourceID).Scan(&status, &gotCategory, &sourceStatus, &reviewType))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(metadata_json->'residual_dimensions')='array' THEN metadata_json->'residual_dimensions' ELSE '[]'::jsonb END)),'{}') FROM transaction_proposal WHERE source_event_id=$1`, f.sourceID).Scan(&residual))
	wantCategory, wantSource, wantReview := "food-drink", "PROCESSED", ""
	if wantStatus == "NEEDS_REVIEW" {
		wantCategory, wantSource, wantReview = "", "NEEDS_REVIEW", "AMBIGUOUS_CATEGORY"
	}
	if status != wantStatus || gotCategory != wantCategory || sourceStatus != wantSource || reviewType != wantReview {
		t.Fatalf("canonical transaction: status=%s category=%q source=%s review=%q; want %s/%q/%s/%q", status, gotCategory, sourceStatus, reviewType, wantStatus, wantCategory, wantSource, wantReview)
	}
	if strings.Join(residual, ",") != strings.Join(wantResidual, ",") {
		t.Fatalf("residual dimensions=%v, want %v", residual, wantResidual)
	}
}

func TestTelegramComplexClearExtractionConfirmsWithOneAgentCall(t *testing.T) {
	runIR04ComplexTransaction(t, "food-drink", "CONFIRMED", nil)
}

// With no accepted category the transaction is parked, never guessed: Go records
// the unresolved dimension and the transaction waits for review.
func TestTelegramCategoryResidualParksTransactionForReview(t *testing.T) {
	runIR04ComplexTransaction(t, "", "NEEDS_REVIEW", []string{"category"})
}

// A real open review must survive a new expense in the same chat.
func TestTelegramT2NewExpenseDoesNotResolveOpenReview(t *testing.T) {
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
	if err := processor.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var confirmed, newExpenseNeedsReview, oldExpenseNeedsReview, reviewStatus, reviewRequestStatus int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE type='EXPENSE' AND status='CONFIRMED' AND amount=5000),count(*) FILTER(WHERE type='EXPENSE' AND status='NEEDS_REVIEW' AND amount=5000),count(*) FILTER(WHERE id=$2 AND status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, f.householdID, oldTransaction).Scan(&confirmed, &newExpenseNeedsReview, &oldExpenseNeedsReview))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item i JOIN review_request r ON r.review_item_id=i.id WHERE r.id=$1 AND i.status='OPEN'`, reviewID).Scan(&reviewStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE id=$1 AND status='OPEN'`, reviewID).Scan(&reviewRequestStatus))
	if confirmed != 1 || newExpenseNeedsReview != 0 || oldExpenseNeedsReview != 1 || reviewStatus != 1 || reviewRequestStatus != 1 {
		t.Fatalf("confirmed_new=%d new_expense_needs_review=%d existing_needs_review=%d review_item_open=%d request_open=%d", confirmed, newExpenseNeedsReview, oldExpenseNeedsReview, reviewStatus, reviewRequestStatus)
	}
}

// An exact reply to a category review resolves that review only.
func TestTelegramT3ExactCategoryReplyResolvesBoundReview(t *testing.T) {
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
