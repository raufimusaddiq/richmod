package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAgentRecordTransactionRecordsSimilarExpenseWithDuplicateReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "no-implicit-correction")
	var categoryID, existingID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `
		INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,counterparty_name,description,created_by_user_id,confirmed_at)
		VALUES($1,'EXPENSE','CONFIRMED',83000,'IDR',now()-interval '1 day',$2,'Gacoan','makan kemarin',$3,now()-interval '1 day') RETURNING id`, f.householdID, categoryID, f.userID).Scan(&existingID))

	p := NewProcessor(f.pool, nil)
	f.state.Update.Message.Text = "Gacoan 83000 hari ini"
	result, synthesize, err := p.agentRecordTransaction(ctx, f.state, gateway.ToolCall{CallID: "new-gacoan", Name: "record_transaction"}, map[string]any{
		"type":                "EXPENSE",
		"amount_idr":          "83000",
		"merchant":            "Gacoan",
		"category_slug":       "dining",
		"description":         "makan hari ini",
		"note":                nil,
		"date_reference":      "TODAY",
		"date_provenance":     "USER_STATED",
		"explicit_date":       nil,
		"local_time":          "12:30",
		"ambiguous":           false,
		"confidence":          1.0,
		"category_confidence": 1.0,
	}, gateway.Metadata{Model: "test-model"})
	mustAgentTest(t, err)
	if synthesize || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("unexpected record result: synthesize=%v result=%#v", synthesize, result)
	}
	var transactions, duplicateReviews int
	var carried bool
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND type='EXPENSE' AND amount=83000 AND lower(COALESCE(counterparty_name,''))='gacoan'`, f.householdID).Scan(&transactions))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(decision->'knownFacts'->'prior_match_transaction_ids' ? $2),false) FROM review_item WHERE household_id=$1 AND review_type='POSSIBLE_DUPLICATE' AND status IN ('OPEN','PENDING_SEND')`, f.householdID, existingID).Scan(&duplicateReviews, &carried))
	if transactions != 2 || duplicateReviews != 1 || !carried {
		t.Fatalf("similar expense was not recorded for review: transactions=%d duplicate_reviews=%d carried=%v", transactions, duplicateReviews, carried)
	}
}

func TestAgentStageBatchStagesUnknownCategoryForReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-invalid-category")
	p := NewProcessor(f.pool, nil)
	result, _, err := p.agentStageBatch(ctx, f.state, gateway.ToolCall{CallID: "bad-batch", Name: "record_transaction_batch"}, map[string]any{
		"items": []any{map[string]any{
			"type":                "EXPENSE",
			"amount_idr":          "50000",
			"merchant":            "Warung",
			"category_slug":       "does-not-exist",
			"description":         "makan",
			"note":                nil,
			"date_reference":      "TODAY",
			"date_provenance":     "USER_STATED",
			"explicit_date":       nil,
			"local_time":          "12:00",
			"confidence":          1.0,
			"category_confidence": 1.0,
		}},
	})
	mustAgentTest(t, err)
	items, _ := result.Mutation["items"].([]map[string]any)
	if result.Status != "PENDING_CONFIRMATION" || len(items) != 1 || items[0]["category_slug"] != "" {
		t.Fatalf("unexpected staged result: %#v", result)
	}
	var batches int
	var sourceStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING'`, f.householdID, f.chatID, f.chatID).Scan(&batches))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, f.sourceID).Scan(&sourceStatus))
	if batches != 1 || sourceStatus != "PROCESSED" {
		t.Fatalf("batch staging: batches=%d source=%s", batches, sourceStatus)
	}
	var reply string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 ORDER BY created_at DESC LIMIT 1`, f.chatID).Scan(&reply))
	if !strings.Contains(reply, "Warung · Rp50.000") || !strings.Contains(reply, "kategori belum ada") {
		t.Fatalf("staged list reply = %q", reply)
	}
}
