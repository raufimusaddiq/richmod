package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestStandaloneMerchantReviewAnswerForOwnerAndMember(t *testing.T) {
	for _, role := range []string{"OWNER", "MEMBER"} {
		t.Run(role, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "standalone-merchant-"+strings.ToLower(role))
			_, err := f.pool.Exec(ctx, `UPDATE household_member SET role=$3 WHERE household_id=$1 AND user_id=$2`, f.householdID, f.userID, role)
			mustAgentTest(t, err)
			reviewID, transactionID := createAgentTransactionReview(t, ctx, f, "36500", 842)
			_, err = f.pool.Exec(ctx, `UPDATE review_request SET review_type='UNKNOWN_MERCHANT' WHERE id=$1`, reviewID)
			mustAgentTest(t, err)
			_, err = f.pool.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_MERCHANT' WHERE review_request_id=$1`, reviewID)
			mustAgentTest(t, err)
			model := &capturingGateway{script: []gateway.AgentResponse{
				{ToolCalls: []gateway.ToolCall{{CallID: "merchant-answer", Name: "resolve_review", Arguments: json.RawMessage(`{"action":"CONFIRM","merchant":"grab"}`)}}},
				{Text: "Detail review sudah diperbarui."},
			}}
			p, engine := evidenceTurnProcessor(f, model, "REVIEW_INTERACTION")
			sourceID, err := runEvidenceTurn(t, ctx, f, p, "grab", 0)
			mustAgentTest(t, err)
			payload := engine.requests[0].State.(map[string]any)
			if payload["active_review_count"] != 1 || payload["active_review"].(map[string]any)["awaiting_field"] != "merchant" {
				t.Fatalf("route lacks merchant review context: %v", payload)
			}
			raw, err := json.Marshal(payload)
			mustAgentTest(t, err)
			for _, id := range []string{reviewID, transactionID, f.householdID, f.userID} {
				if strings.Contains(string(raw), id) {
					t.Fatal("route request exposes a canonical ID")
				}
			}
			turn := model.turnContext(t, 0)
			if turn["workflow_scope"] != string(agentWorkflowUniqueReview) || !model.toolNames(0)["resolve_review"] || model.toolNames(0)["record_transaction"] {
				t.Fatalf("wrong workflow: %v tools=%v", turn, model.toolNames(0))
			}
			var status, merchant, conversation string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(m.normalized_name,''),c.state FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id JOIN review_request r ON r.transaction_id=t.id JOIN review_conversation c ON c.review_request_id=r.id WHERE t.id=$1`, transactionID).Scan(&status, &merchant, &conversation))
			if status != "NEEDS_REVIEW" || merchant != "grab" || conversation != "AWAITING_CATEGORY" {
				t.Fatalf("status=%s merchant=%s conversation=%s", status, merchant, conversation)
			}
			if countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID) != 1 || countRows(t, ctx, f, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND source_event_id=$2`, transactionID, sourceID) != 1 {
				t.Fatal("answer duplicated a transaction or failed to preserve evidence")
			}
		})
	}
}

func TestStandaloneReviewRejectedRouteKeepsReviewUntouched(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "standalone-rejected")
	reviewID, transactionID := createAgentTransactionReview(t, ctx, f, "36500", 842)
	model := &capturingGateway{}
	p := NewProcessor(f.pool, model)
	p.SetJudgment(&stubJudgmentEngine{}) // No acceptable route answer.
	_, err := runEvidenceTurn(t, ctx, f, p, "grab", 0)
	mustAgentTest(t, err)
	if model.turnContext(t, 0)["mutation_authority_unavailable"] != true || len(sideEffectNames(model.requests[0].Tools)) != 0 {
		t.Fatal("a rejected route granted mutation authority")
	}
	var reviewStatus, transactionStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT r.status,t.status FROM review_request r JOIN transaction t ON t.id=r.transaction_id WHERE r.id=$1 AND t.id=$2`, reviewID, transactionID).Scan(&reviewStatus, &transactionStatus))
	if reviewStatus != "OPEN" || transactionStatus != "NEEDS_REVIEW" {
		t.Fatalf("rejected route changed review=%s transaction=%s", reviewStatus, transactionStatus)
	}
}

func TestStandaloneReviewAmbiguityOnlyListsReviews(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "standalone-ambiguous")
	createAgentTransactionReview(t, ctx, f, "36500", 842)
	createAgentTransactionReview(t, ctx, f, "70000", 844)
	model := &capturingGateway{}
	p, _ := evidenceTurnProcessor(f, model, "REVIEW_INTERACTION")
	_, err := runEvidenceTurn(t, ctx, f, p, "grab", 0)
	mustAgentTest(t, err)
	if len(model.requests) != 0 || countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW' AND merchant_id IS NULL`, f.householdID) != 2 {
		t.Fatal("ambiguous reviews reached a mutation lane or changed transactions")
	}
}

func createAgentTransactionReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, amount string, messageID int64) (string, string) {
	t.Helper()
	var transactionID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',$2,'IDR',now(),'binding test',$3) RETURNING id`, f.householdID, amount, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN',$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, f.chatID, messageID)
	mustAgentTest(t, err)
	return reviewID, transactionID
}

func TestAgentReviewBindingExactReplyWinsOverOtherOpenReview(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "binding-exact")
	firstReview, _ := createAgentTransactionReview(t, ctx, f, "70000", 101)
	createAgentTransactionReview(t, ctx, f, "90000", 202)

	update := f.update
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 101}
	p := NewProcessor(f.pool, nil)
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, update)
	mustAgentTest(t, err)
	if count != 1 || binding == nil {
		t.Fatalf("exact reply binding = %#v count=%d; want one target", binding, count)
	}
	if binding.Kind != "TRANSACTION" || binding.ReviewRequestID != firstReview {
		t.Fatalf("exact reply resolved %#v; want review %s", binding, firstReview)
	}
}

func TestAgentReviewBindingAmbiguityFailsClosed(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "binding-ambiguous")
	createAgentTransactionReview(t, ctx, f, "70000", 101)
	createAgentTransactionReview(t, ctx, f, "90000", 202)

	p := NewProcessor(f.pool, nil)
	binding, public, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding != nil || count != 2 {
		t.Fatalf("ambiguous binding=%#v count=%d public=%#v; want nil/2", binding, count, public)
	}
}

func TestBoundReviewTargetDoesNotDriftWhenAnotherReviewAppears(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "binding-drift")
	_, firstTransaction := createAgentTransactionReview(t, ctx, f, "70000", 101)
	p := NewProcessor(f.pool, nil)
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 {
		t.Fatalf("initial binding=%#v count=%d", binding, count)
	}

	_, secondTransaction := createAgentTransactionReview(t, ctx, f, "90000", 202)
	state := f.state
	state.ReviewBinding = binding
	state.ReviewBindingCount = 1
	result, synthesize, err := p.agentResolveBoundReview(ctx, state, gateway.ToolCall{CallID: "bound-ignore", Name: "resolve_review"}, map[string]any{"action": "IGNORE"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "RESOLVED" {
		t.Fatalf("bound resolution synthesize=%v result=%#v", synthesize, result)
	}
	var firstStatus, secondStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, firstTransaction).Scan(&firstStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, secondTransaction).Scan(&secondStatus))
	if firstStatus != "VOIDED" || secondStatus != "NEEDS_REVIEW" {
		t.Fatalf("bound target drifted: first=%s second=%s", firstStatus, secondStatus)
	}
}

func createMerchantLearningReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, messageID int64, merchantName string) (string, string) {
	t.Helper()
	var categoryID, merchantID, transactionID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, f.householdID, "Dining "+merchantName, "dining-"+merchantName).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, f.householdID, merchantName).Scan(&merchantID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',50000,'IDR',now(),$2,$3,$4,now()) RETURNING id`, f.householdID, merchantID, categoryID, f.userID).Scan(&transactionID))
	// Confirm completes the item immediately; only the optional
	// merchant-learning question keeps the conversation in a pending state.
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,resolved_at,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','RESOLVED',now(),$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_MERCHANT_DECISION')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, f.chatID, messageID)
	mustAgentTest(t, err)
	return reviewID, transactionID
}

func TestMerchantLearningRequiresUniqueOrExactBinding(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "merchant-binding")
	firstReview, _ := createMerchantLearningReview(t, ctx, f, 301, "merchanta")
	createMerchantLearningReview(t, ctx, f, 302, "merchantb")
	p := NewProcessor(f.pool, nil)

	binding, count, err := p.loadAgentMerchantLearningBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding != nil || count != 2 {
		t.Fatalf("ambiguous merchant binding=%#v count=%d; want nil/2", binding, count)
	}

	update := f.update
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 301}
	binding, count, err = p.loadAgentMerchantLearningBinding(ctx, f.householdID, update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 || binding.ReviewRequestID != firstReview {
		t.Fatalf("exact merchant binding=%#v count=%d; want %s", binding, count, firstReview)
	}
}
