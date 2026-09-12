package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAgentRecordTransactionNeverImplicitlyCorrectsSimilarExpense(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "no-implicit-correction")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))
	_, err := f.pool.Exec(ctx, `
		INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,counterparty_name,description,created_by_user_id,confirmed_at)
		VALUES($1,'EXPENSE','CONFIRMED',83000,'IDR',now()-interval '1 day',$2,'Gacoan','makan kemarin',$3,now()-interval '1 day')`, f.householdID, categoryID, f.userID)
	mustAgentTest(t, err)

	p := NewProcessor(f.pool, nil)
	result, synthesize, err := p.agentRecordTransaction(ctx, f.state, gateway.ToolCall{CallID: "new-gacoan", Name: "record_transaction"}, map[string]any{
		"type":                "EXPENSE",
		"amount_idr":          "83000",
		"merchant":            "Gacoan",
		"category_slug":       "dining",
		"description":         "makan hari ini",
		"note":                nil,
		"date_reference":      "TODAY",
		"explicit_date":       nil,
		"local_time":          "12:30",
		"confidence":          1.0,
		"category_confidence": 1.0,
	}, gateway.Metadata{Model: "test-model"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "CONFIRMED" || result.Mutation["action"] != "TRANSACTION_RECORDED" {
		t.Fatalf("unexpected record result: synthesize=%v result=%#v", synthesize, result)
	}
	var transactions, pendingCorrections int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND type='EXPENSE' AND amount=83000 AND lower(COALESCE(counterparty_name,''))='gacoan'`, f.householdID).Scan(&transactions))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_pending_action WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING'`, f.householdID, f.chatID, f.chatID).Scan(&pendingCorrections))
	if transactions != 2 || pendingCorrections != 0 {
		t.Fatalf("similar expense was treated as correction: transactions=%d pending=%d", transactions, pendingCorrections)
	}
}

func TestAgentStageBatchRejectsUnknownCategoryWithoutState(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-invalid-category")
	p := NewProcessor(f.pool, nil)
	result, synthesize, err := p.agentStageBatch(ctx, f.state, gateway.ToolCall{CallID: "bad-batch", Name: "record_transaction_batch"}, map[string]any{
		"items": []any{map[string]any{
			"type":                "EXPENSE",
			"amount_idr":          "50000",
			"merchant":            "Warung",
			"category_slug":       "does-not-exist",
			"description":         "makan",
			"note":                nil,
			"date_reference":      "TODAY",
			"explicit_date":       nil,
			"local_time":          "12:00",
			"confidence":          1.0,
			"category_confidence": 1.0,
		}},
	})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "INVALID_CATEGORY" {
		t.Fatalf("unexpected invalid category result: synthesize=%v result=%#v", synthesize, result)
	}
	var batches int
	var sourceStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3`, f.householdID, f.chatID, f.chatID).Scan(&batches))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, f.sourceID).Scan(&sourceStatus))
	if batches != 0 || sourceStatus != "RECEIVED" {
		t.Fatalf("invalid batch leaked state: batches=%d source=%s", batches, sourceStatus)
	}
}

func TestAgentWealthAssetPurchaseCommitsOneAtomicOutcome(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "wealth-asset-atomic")
	var accountID, wealthID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&accountID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Bibit','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, f.householdID).Scan(&wealthID))
	created := createAgentWealthReviewForChat(t, ctx, f, f.chatID, 701)

	p := NewProcessor(f.pool, nil)
	binding, err := p.exactAgentReviewBinding(ctx, f.householdID, f.chatID, 701)
	mustAgentTest(t, err)
	if binding == nil || binding.TargetID != created.observationID {
		t.Fatalf("binding=%#v", binding)
	}
	state := *f.state
	state.ReviewBinding = binding
	state.ReviewBindingCount = 1
	state.Update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 701}
	at := time.Now().In(jakartaLocation()).Truncate(time.Minute)
	result, synthesize, err := p.agentResolveBoundWealthAssetPurchaseAtomic(ctx, &state, gateway.ToolCall{CallID: "asset-purchase", Name: "resolve_review"}, map[string]any{
		"action":               "RECORD_ASSET_PURCHASE",
		"source_account_hint":  "Jago",
		"wealth_account_hint":  "Bibit",
		"amount_idr":           "5000000",
		"transaction_at":       at.Format(time.RFC3339),
	}, binding)
	mustAgentTest(t, err)
	if !synthesize || result.Status != "RESOLVED" || result.Mutation["action"] != "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE" {
		t.Fatalf("unexpected asset purchase result: synthesize=%v result=%#v", synthesize, result)
	}
	var transactions int
	var observationStatus, reviewStatus, sourceStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND account_id=$2 AND type='TRANSFER' AND status='CONFIRMED' AND purpose='ASSET_PURCHASE' AND related_wealth_account_id=$3 AND amount=5000000`, f.householdID, accountID, wealthID).Scan(&transactions))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM wealth_observation WHERE id=$1`, created.observationID).Scan(&observationStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, created.reviewID).Scan(&reviewStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, f.sourceID).Scan(&sourceStatus))
	if transactions != 1 || observationStatus != "DISMISSED" || reviewStatus != "RESOLVED" || sourceStatus != "PROCESSED" || len(result.References) != 1 {
		t.Fatalf("inconsistent atomic outcome tx=%d observation=%s review=%s source=%s refs=%#v", transactions, observationStatus, reviewStatus, sourceStatus, result.References)
	}
}

func TestAgentWealthAssetPurchaseRollsBackTransferWhenLaterWriteFails(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "wealth-asset-rollback")
	var accountID, wealthID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&accountID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Bibit','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, f.householdID).Scan(&wealthID))
	created := createAgentWealthReviewForChat(t, ctx, f, f.chatID, 702)
	p := NewProcessor(f.pool, nil)
	binding, err := p.exactAgentReviewBinding(ctx, f.householdID, f.chatID, 702)
	mustAgentTest(t, err)
	state := *f.state
	state.ReviewBinding = binding
	state.ReviewBindingCount = 1
	state.Update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 702}
	// Use a syntactically valid but nonexistent source_event UUID. The transfer
	// insert happens before evidence persistence, so the FK failure proves the
	// whole logical side effect rolls back rather than leaving a partial transfer.
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&state.SourceEventID))
	at := time.Now().In(jakartaLocation()).Truncate(time.Minute)
	_, _, err = p.agentResolveBoundWealthAssetPurchaseAtomic(ctx, &state, gateway.ToolCall{CallID: "asset-purchase-fail", Name: "resolve_review"}, map[string]any{
		"action":              "RECORD_ASSET_PURCHASE",
		"source_account_hint": "Jago",
		"wealth_account_hint": "Bibit",
		"amount_idr":          "5000000",
		"transaction_at":      at.Format(time.RFC3339),
	}, binding)
	if err == nil {
		t.Fatal("expected evidence FK failure")
	}
	var transactions int
	var observationStatus, reviewStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND account_id=$2 AND purpose='ASSET_PURCHASE' AND related_wealth_account_id=$3`, f.householdID, accountID, wealthID).Scan(&transactions))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM wealth_observation WHERE id=$1`, created.observationID).Scan(&observationStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, created.reviewID).Scan(&reviewStatus))
	if transactions != 0 || observationStatus != "PENDING" || reviewStatus != "OPEN" {
		t.Fatalf("partial asset purchase leaked after rollback: tx=%d observation=%s review=%s", transactions, observationStatus, reviewStatus)
	}
}
