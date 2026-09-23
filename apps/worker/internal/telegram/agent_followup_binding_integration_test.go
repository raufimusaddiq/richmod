package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// A re-sent or double-fired expense line must not create a second canonical
// row. The guard surfaces the existing row as a model-safe ref instead.
func TestAgentRecordTransactionDeduplicatesRecentExpense(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "dedup-expense")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Camilan','camilan') RETURNING id`, f.householdID).Scan(&categoryID))
	var existingID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `
		INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id)
		VALUES($1,'EXPENSE','NEEDS_REVIEW',5000,'IDR',now()-interval '1 minute','Jajan gorengan',$2) RETURNING id`, f.householdID, f.userID).Scan(&existingID))

	p := NewProcessor(f.pool, nil)
	p.SetJudgment(clearPurchaseJudgmentEngine{t: t})
	result, synthesize, err := p.agentRecordTransaction(ctx, f.state, gateway.ToolCall{CallID: "dup", Name: "record_transaction"}, map[string]any{
		"type": "EXPENSE", "amount_idr": "5000", "merchant": "Ibu kantin", "category_slug": "camilan",
		"description": "Jajan gorengan", "note": nil, "date_reference": "TODAY", "explicit_date": nil,
		"local_time": "12:30", "confidence": 1.0, "category_confidence": 1.0,
	}, gateway.Metadata{Model: "test-model"})
	mustAgentTest(t, err)
	if synthesize || result.Status != "NO_OP_DUPLICATE" || result.Mutation["action"] != "DUPLICATE_EXPENSE_DETECTED" {
		t.Fatalf("expected dedup, got synthesize=%v result=%#v", synthesize, result)
	}
	var count int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND type='EXPENSE' AND amount=5000`, f.householdID).Scan(&count))
	if count != 1 {
		t.Fatalf("expected exactly one expense row, got %d", count)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected one reference to the existing row, got %#v", result.References)
	}
	resolved, err := p.resolveTransactionReference(ctx, f.householdID, f.update, result.References[0].Ref)
	mustAgentTest(t, err)
	if resolved != existingID {
		t.Fatalf("dedup ref resolved to %s, want %s", resolved, existingID)
	}
}

// Amount alone is not identity. Two genuinely distinct same-amount purchases
// must both be recorded; only an event with matching text is treated as a
// duplicate.
func TestAgentRecordTransactionKeepsDistinctSameAmountExpenses(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "distinct-same-amount")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Camilan','camilan') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, func() error {
		_, err := f.pool.Exec(ctx, `
			INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id)
			VALUES($1,'EXPENSE','NEEDS_REVIEW',5000,'IDR',now()-interval '1 minute','Kopi',$2)`, f.householdID, f.userID)
		return err
	}())

	p := NewProcessor(f.pool, nil)
	p.SetJudgment(clearPurchaseJudgmentEngine{t: t})
	result, synthesize, err := p.agentRecordTransaction(ctx, f.state, gateway.ToolCall{CallID: "distinct", Name: "record_transaction"}, map[string]any{
		"type": "EXPENSE", "amount_idr": "5000", "merchant": "", "category_slug": "camilan",
		"description": "Jajan gorengan", "note": nil, "date_reference": "TODAY", "explicit_date": nil,
		"local_time": "12:30", "confidence": 1.0, "category_confidence": 1.0,
	}, gateway.Metadata{Model: "test-model"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "CONFIRMED" || result.Mutation["action"] != "TRANSACTION_RECORDED" {
		t.Fatalf("distinct same-amount expense was wrongly deduplicated: synthesize=%v result=%#v", synthesize, result)
	}
	var count int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND type='EXPENSE' AND amount=5000`, f.householdID).Scan(&count))
	if count != 2 {
		t.Fatalf("expected two distinct expense rows, got %d", count)
	}
}

// The turn context must expose recent ledger rows so a follow-up ("ibu kantin")
// can bind to the pending transaction instead of being dropped.
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
