package telegram

import (
	"context"
	"testing"
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
