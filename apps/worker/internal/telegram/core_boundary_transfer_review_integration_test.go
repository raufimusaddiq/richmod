package telegram

import (
	"context"
	"testing"
)

func createAgentTransferClassificationReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, messageID int64) (reviewID, transactionID, itemID string) {
	return createTransferClassificationReviewWithType(t, ctx, f, "EXPENSE", messageID)
}

func createTransferClassificationReviewWithType(t *testing.T, ctx context.Context, f agentIntegrationFixture, transactionType string, messageID int64) (reviewID, transactionID, itemID string) {
	t.Helper()
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id) VALUES($1,$2,'NEEDS_REVIEW',4000000,'IDR',now(),'transfer review',$3) RETURNING id`, f.householdID, transactionType, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","allowedActions":["CLASSIFY_TRANSFER","OWN_ACCOUNT","HOUSEHOLD_ACCOUNT","INVESTMENT_ACCOUNT","EXPENSE","ASSET_PURCHASE","IGNORE"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION','OPEN',$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_PURPOSE')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, f.chatID, messageID)
	mustAgentTest(t, err)
	return reviewID, transactionID, itemID
}
