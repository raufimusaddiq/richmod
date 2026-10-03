package telegram

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func correctionCallFromEvidence(t *testing.T, request gateway.AgentRequest, category string, override string) gateway.AgentResponse {
	t.Helper()
	ref := override
	if ref == "" {
		content, _ := request.Content.(map[string]any)
		turn, _ := content["turn_context"].(map[string]any)
		bound, _ := turn["bound_evidence"].(map[string]any)
		canonical, _ := bound["canonical"].(map[string]any)
		ref, _ = canonical["transaction_ref"].(string)
		if ref == "" {
			t.Fatalf("the bound evidence carries no canonical transaction_ref: %v", bound)
		}
	}
	args, _ := json.Marshal(map[string]any{"target_ref": ref, "category_slug": category, "period": "THIS_MONTH"})
	return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "correct", Name: "propose_transaction_correction", Arguments: args}}}
}

func TestCategoryReplyToALinkedReceiptStagesACorrectionOnThatTransactionOnly(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "corr-linked")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
	b := seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102, linkTransaction: true})
	var categoryID, originalCategory string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT category_id::text FROM transaction WHERE id=$1`, a.transactionID).Scan(&originalCategory))
	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return correctionCallFromEvidence(t, request, "makan", "")
		}
		return gateway.AgentResponse{Text: "Kategori diubah setelah kamu konfirmasi."}
	}}
	p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")

	before := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID)
	_, err := runEvidenceTurn(t, ctx, f, p, "yang ini salah kategorinya, makan", 101) // reply to receipt A, with receipt B also in the chat
	mustAgentTest(t, err)

	var staged int
	var stagedTransaction, stagedCategory, status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) OVER(),transaction_id::text,COALESCE(proposed_category_id::text,''),status FROM telegram_pending_action WHERE household_id=$1`, f.householdID).Scan(&staged, &stagedTransaction, &stagedCategory, &status))
	if staged != 1 || stagedTransaction != a.transactionID || stagedCategory != categoryID || status != "PENDING" {
		t.Fatalf("staged=%d transaction=%s category=%s status=%s; want one pending correction on receipt A's transaction", staged, stagedTransaction, stagedCategory, status)
	}
	if stagedTransaction == b.transactionID {
		t.Fatal("the correction targeted receipt B")
	}
	// Staging is not applying: the household confirms before anything changes, and
	// no second ledger row appears.
	var nowCategory string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT category_id::text FROM transaction WHERE id=$1`, a.transactionID).Scan(&nowCategory))
	if nowCategory != originalCategory {
		t.Fatalf("a staged correction already changed the category: %s -> %s", originalCategory, nowCategory)
	}
	if after := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID); after != before {
		t.Fatalf("the correction created %d new transactions", after-before)
	}
}

func TestCorrectionRefusesATargetThatIsNotAnIssuedRefForThisHousehold(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "corr-foreign")
	seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
	foreign := newAgentIntegrationFixture(t, "corr-foreign-other")
	foreignEvidence := seedEvidence(t, ctx, foreign, "x", evidenceSeedOptions{amount: "999000", merchant: "Other", messageID: 101, linkTransaction: true})
	// The category exists, so a refusal can only be about the target.
	if _, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan')`, f.householdID); err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]string{
		"another household's transaction id": foreignEvidence.transactionID,
		"a made-up ref":                      "a00000000_p0r0_tx1",
	} {
		ref := ref
		model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
			if call == 0 {
				return correctionCallFromEvidence(t, request, "makan", ref)
			}
			return gateway.AgentResponse{Text: "Tidak bisa."}
		}}
		p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")
		_, _ = runEvidenceTurn(t, ctx, f, p, "salah kategori", 101)
		if n := countRows(t, ctx, f, `SELECT count(*) FROM telegram_pending_action WHERE household_id=$1`, f.householdID); n != 0 {
			t.Fatalf("%s: a correction was staged for an unissued target", name)
		}
	}
}
