package telegram

import (
	"context"
	"testing"
)

func TestStandaloneReviewUntouchedWhenModelDoesNotResolveIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "standalone-untouched")
	reviewID, transactionID := createAgentTransactionReview(t, ctx, f, "36500", 842)
	model := &capturingGateway{}
	p := NewProcessor(f.pool, model)
	_, err := runEvidenceTurn(t, ctx, f, p, "grab", 0)
	mustAgentTest(t, err)
	if len(model.requests) == 0 {
		t.Fatal("the turn did not reach the agent")
	}
	var reviewStatus, transactionStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT r.status,t.status FROM review_request r JOIN transaction t ON t.id=r.transaction_id WHERE r.id=$1 AND t.id=$2`, reviewID, transactionID).Scan(&reviewStatus, &transactionStatus))
	if reviewStatus != "OPEN" || transactionStatus != "NEEDS_REVIEW" {
		t.Fatalf("a turn with no resolving tool call changed review=%s transaction=%s", reviewStatus, transactionStatus)
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

func createMerchantLearningReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, messageID int64, merchantName string) (string, string) {
	t.Helper()
	var categoryID, merchantID, transactionID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, f.householdID, "Dining "+merchantName, "dining-"+merchantName).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, f.householdID, merchantName).Scan(&merchantID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',50000,'IDR',now(),$2,$3,$4,now()) RETURNING id`, f.householdID, merchantID, categoryID, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,resolved_at,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','RESOLVED',now(),$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_MERCHANT_DECISION')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, f.chatID, messageID)
	mustAgentTest(t, err)
	return reviewID, transactionID
}

func requireCategoryStep(t *testing.T, ctx context.Context, f agentIntegrationFixture, reviewID string) {
	t.Helper()
	_, err := f.pool.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{missingFacts}','["category"]'::jsonb) WHERE id=(SELECT review_item_id FROM review_request WHERE id=$1)`, reviewID)
	mustAgentTest(t, err)
}
