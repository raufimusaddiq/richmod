package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func createAgentTransactionReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, amount string, messageID int64) (string, string) {
	t.Helper()
	var transactionID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',$2,'IDR',now(),'binding test',$3) RETURNING id`, f.householdID, amount, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, f.householdID, transactionID).Scan(&itemID))
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
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN',$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CONFIRMATION')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, f.chatID, messageID)
	mustAgentTest(t, err)
	return reviewID, transactionID
}

func TestMerchantLearningRequiresUniqueOrExactBinding(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "merchant-binding")
	firstReview, _ := createMerchantLearningReview(t, ctx, f, 301, "MerchantA")
	createMerchantLearningReview(t, ctx, f, 302, "MerchantB")
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
