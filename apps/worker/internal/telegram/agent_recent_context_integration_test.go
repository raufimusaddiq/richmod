package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// The turn context must expose recent ledger rows so a follow-up ("ibu kantin")
// can bind to the transaction that was just recorded instead of being dropped.
// Rows are surfaced with opaque refs only: the model can reference an existing
// row but the context never leaks a canonical UUID.
func TestAgentTurnContextIncludesRecentTransactions(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-ctx")
	mustAgentTest(t, func() error {
		_, err := f.pool.Exec(ctx, `
			INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,counterparty_name,created_by_user_id)
			VALUES($1,'EXPENSE','NEEDS_REVIEW',5000,'IDR',now()-interval '2 minutes','Jajan gorengan','',$2)`, f.householdID, f.userID)
		return err
	}())

	p := NewProcessor(f.pool, nil)
	state, err := p.loadAgentContextState(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if len(state.RecentTransactions) != 1 {
		t.Fatalf("expected one recent transaction in context, got %d", len(state.RecentTransactions))
	}
	recent := state.RecentTransactions[0]
	if recent["amount_idr"] != "5000" || recent["status"] != "NEEDS_REVIEW" {
		t.Fatalf("unexpected recent transaction context: %#v", recent)
	}
	ref, _ := recent["ref"].(string)
	if ref == "" {
		t.Fatalf("recent transaction missing model-safe ref: %#v", recent)
	}
	if _, err := p.resolveTransactionReference(ctx, f.householdID, f.update, ref); err != nil {
		t.Fatalf("recent ref did not resolve: %v", err)
	}
}

// A repeated same-amount expense is still a real purchase: the recent-context
// aid must not suppress recording, so two identical lines produce two rows.
func TestAgentRecordTransactionKeepsRepeatedSameAmountExpense(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "repeat-same-amount")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Camilan','camilan') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, func() error {
		_, err := f.pool.Exec(ctx, `
			INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id,confirmed_at)
			VALUES($1,'EXPENSE','CONFIRMED',5000,'IDR',now()-interval '1 minute','Kopi',$2,now()-interval '1 minute')`, f.householdID, f.userID)
		return err
	}())

	p := NewProcessor(f.pool, nil)
	p.SetJudgment(clearPurchaseJudgmentEngine{t: t})
	_, _, err := p.agentRecordTransaction(ctx, f.state, gateway.ToolCall{CallID: "second-kopi", Name: "record_transaction"}, map[string]any{
		"type": "EXPENSE", "amount_idr": "5000", "merchant": "", "category_slug": "camilan",
		"description": "Kopi", "note": nil, "date_reference": "TODAY", "explicit_date": nil,
		"local_time": "12:30", "confidence": 1.0, "category_confidence": 1.0,
	}, gateway.Metadata{Model: "test-model"})
	mustAgentTest(t, err)
	var count int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND type='EXPENSE' AND amount=5000 AND COALESCE(description,'')='Kopi'`, f.householdID).Scan(&count))
	if count != 2 {
		t.Fatalf("expected both same-amount purchases recorded, got %d", count)
	}
}
