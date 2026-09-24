package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// IR-02 root fix: the conversational agent lane reaches the same canonical
// confirm as the generic reply lane, so its CONFIRM must also refuse while the
// stored ReviewDecision still names a required residual fact.
func TestAgentConfirmRefusesWhenStoredResidualDateIsUnsupplied(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "residual-guard")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining-guard') RETURNING id`, f.householdID).Scan(&categoryID))
	var transactionID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',42000,'IDR',now(),$2) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"missingFacts":["transaction_at"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN',$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, reviewID)
	mustAgentTest(t, err)

	p := NewProcessor(f.pool, nil)
	review := agentTransactionReview{reviewID: reviewID, transactionID: transactionID, reviewType: "AMBIGUOUS_CATEGORY", conversationState: "AWAITING_CATEGORY"}
	result, _, err := p.agentConfirmTransactionReview(ctx, f.state, gateway.ToolCall{CallID: "guard", Name: "resolve_review"}, review, categoryID, reviewExtraction{Confidence: 1})
	if err != nil || result.Status != "RESIDUAL_FACTS_REQUIRED" {
		t.Fatalf("missing date result=%+v err=%v; want review result", result, err)
	}
	if got := agentMutationFallback(result); got != "Tinjauan ini masih menunggu tanggal transaksi. Balas dengan nilai itu untuk menyelesaikan." {
		t.Fatalf("fallback=%q; want explicit date request", got)
	}
	if _, _, err := p.agentConfirmTransactionReview(ctx, f.state, gateway.ToolCall{CallID: "guard-invalid-date", Name: "resolve_review"}, review, categoryID, reviewExtraction{Confidence: 1, PayDate: "2026-02-30"}); err == nil {
		t.Fatal("invalid supplied date must be rejected")
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&status))
	if status != "NEEDS_REVIEW" {
		t.Fatalf("transaction status=%s; the residual guard must leave it in review", status)
	}
	if _, _, err := p.agentConfirmTransactionReview(ctx, f.state, gateway.ToolCall{CallID: "guard-date", Name: "resolve_review"}, review, categoryID, reviewExtraction{Confidence: 1, PayDate: "2026-03-04"}); err != nil {
		t.Fatalf("a valid supplied date must satisfy the stored residual: %v", err)
	}
	var transactionAt time.Time
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT transaction_at FROM transaction WHERE id=$1`, transactionID).Scan(&transactionAt))
	if transactionAt.In(jakartaLocation()).Format("2006-01-02") != "2026-03-04" {
		t.Fatalf("transaction date=%s; want persisted user-supplied date", transactionAt)
	}
}
