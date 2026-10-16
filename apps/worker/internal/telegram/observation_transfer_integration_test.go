package telegram

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestAuditF12ObservationTransferDoesNotOfferTransactionButtons(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "observation-transfer")
	var observation, item string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,0,'CASH_MOVEMENT','{}','REVIEW') RETURNING id`, f.householdID, f.sourceID).Scan(&observation))
	decision, _ := reviewdec.Preset("TRANSFER_CLASSIFICATION", "financial_email_observation", observation)
	encoded, err := json.Marshal(decision)
	mustAgentTest(t, err)
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN',$3) RETURNING id`, f.householdID, observation, encoded).Scan(&item))
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	defer tx.Rollback(ctx)
	mustAgentTest(t, ProjectReviewItem(ctx, tx, f.householdID, item, 0, "", f.chatID))
	mustAgentTest(t, tx.Commit(ctx))
	var requests int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE review_item_id=$1`, item).Scan(&requests))
	if requests != 0 {
		t.Fatalf("observation-backed transfer produced %d transaction-only Telegram projections", requests)
	}
	var open, transactions int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM review_item WHERE id=$1 AND status='OPEN'),(SELECT count(*) FROM transaction WHERE household_id=$2)`, item, f.householdID).Scan(&open, &transactions))
	if open != 1 || transactions != 0 {
		t.Fatalf("canonical review=%d transactions=%d; Web review must survive without a manufactured transaction", open, transactions)
	}
}
