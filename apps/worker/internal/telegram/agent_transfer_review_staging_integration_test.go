package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAgentStageTransferReviewCreatesChatScopedBinding(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "transfer-stage-binding")
	var accountID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Stage Account','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&accountID))
	p := NewProcessor(f.pool, nil)
	intent := transferReconciliationIntent{accountID: accountID, amount: "125000", at: time.Now().In(jakartaLocation()), description: "transfer review", purpose: "INTERNAL_TRANSFER"}
	result, synthesize, err := p.agentStageTransferReview(ctx, f.state, gateway.ToolCall{CallID: "stage-transfer", Name: "record_transfer"}, intent, nil, "POSSIBLE_DUPLICATE")
	mustAgentTest(t, err)
	if !synthesize || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("unexpected staged result: synthesize=%v result=%#v", synthesize, result)
	}
	var requests, recipients int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE ri.source_event_id=$1 AND r.household_id=$2 AND r.status='OPEN'`, f.sourceID, f.householdID).Scan(&requests))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id JOIN review_item ri ON ri.id=r.review_item_id WHERE ri.source_event_id=$1 AND rr.telegram_chat_id=$2`, f.sourceID, f.chatID).Scan(&recipients))
	if requests != 1 || recipients != 1 {
		t.Fatalf("staged review plumbing requests=%d recipients=%d; want 1/1", requests, recipients)
	}
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 || binding.Kind != "TRANSFER_RECONCILIATION" {
		t.Fatalf("staged binding=%#v count=%d; want one transfer reconciliation", binding, count)
	}
}
