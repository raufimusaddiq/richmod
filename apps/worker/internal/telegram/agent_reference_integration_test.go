package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func recordAgentTransactionForRefTest(t *testing.T, ctx context.Context, f agentIntegrationFixture, sourceID string, update telegramUpdate, amount, merchant string) agentToolResult {
	t.Helper()
	p := NewProcessor(f.pool, nil)
	state := &agentState{
		SourceEventID: sourceID,
		HouseholdID:   f.householdID,
		Update:        update,
		Now:           time.Now().In(jakartaLocation()),
		ModelPhases:   1,
	}
	result, synthesize, err := p.agentRecordTransaction(ctx, state, gateway.ToolCall{CallID: "record-" + amount, Name: "record_transaction"}, map[string]any{
		"type":                "EXPENSE",
		"amount_idr":          amount,
		"merchant":            merchant,
		"category_slug":       "dining",
		"description":         "makan",
		"note":                nil,
		"date_reference":      "TODAY",
		"explicit_date":       nil,
		"local_time":          "12:30",
		"confidence":          1.0,
		"category_confidence": 1.0,
	}, gateway.Metadata{Model: "test-model"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "CONFIRMED" {
		t.Fatalf("record transaction synthesize=%v result=%+v", synthesize, result)
	}
	if len(result.References) != 1 {
		t.Fatalf("record transaction refs=%+v, want exactly one", result.References)
	}
	ref, _ := result.Mutation["ref"].(string)
	if ref == "" || ref != result.References[0].Ref || ref == "tx_1" || !strings.HasPrefix(ref, "a") {
		t.Fatalf("record transaction ref=%q references=%+v", ref, result.References)
	}
	return result
}

func TestAgentRecordedTransactionRefsAreScopedAcrossTurns(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recorded-ref-scope")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)

	first := recordAgentTransactionForRefTest(t, ctx, f, f.sourceID, f.update, "83000", "Gacoan")
	firstRef := first.References[0].Ref
	var firstTransactionID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT transaction_id FROM transaction_evidence WHERE source_event_id=$1 AND evidence_type='TELEGRAM_TEXT' LIMIT 1`, f.sourceID).Scan(&firstTransactionID))

	var secondSourceID string
	stamp := time.Now().UnixNano()
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, f.householdID, fmt.Sprintf("agent-recorded-ref-second-%d", stamp), []byte(fmt.Sprintf("agent-recorded-ref-second-%d", stamp))).Scan(&secondSourceID))
	secondUpdate := f.update
	secondUpdate.Message.MessageID = 2
	second := recordAgentTransactionForRefTest(t, ctx, f, secondSourceID, secondUpdate, "91000", "Bakmi")
	secondRef := second.References[0].Ref
	if firstRef == secondRef {
		t.Fatalf("cross-turn refs collided: %q", firstRef)
	}
	var secondTransactionID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT transaction_id FROM transaction_evidence WHERE source_event_id=$1 AND evidence_type='TELEGRAM_TEXT' LIMIT 1`, secondSourceID).Scan(&secondTransactionID))

	p := NewProcessor(f.pool, nil)
	resolvedFirst, err := p.resolveTransactionReference(ctx, f.householdID, secondUpdate, firstRef)
	mustAgentTest(t, err)
	resolvedSecond, err := p.resolveTransactionReference(ctx, f.householdID, secondUpdate, secondRef)
	mustAgentTest(t, err)
	if resolvedFirst != firstTransactionID || resolvedSecond != secondTransactionID {
		t.Fatalf("scoped ref resolution first=%s/%s second=%s/%s", resolvedFirst, firstTransactionID, resolvedSecond, secondTransactionID)
	}

	var legacyCount int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_turn_reference r JOIN telegram_conversation_turn t ON t.id=r.turn_id WHERE t.source_event_id IN ($1,$2) AND r.ref_key='tx_1'`, f.sourceID, secondSourceID).Scan(&legacyCount))
	if legacyCount != 0 {
		t.Fatalf("recorded transaction created %d legacy tx_1 refs", legacyCount)
	}
}

func TestAgentMutationRefRollsBackWithCanonicalTransaction(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recorded-ref-atomic")
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	var transactionID string
	mustAgentTest(t, tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',42000,'IDR',now(),'atomic ref test',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	refs, err := persistAgentTransactionReferencesTx(ctx, tx, f.householdID, f.sourceID, f.update, "p1r0", []string{transactionID})
	mustAgentTest(t, err)
	if len(refs) != 1 {
		t.Fatalf("refs=%+v, want one", refs)
	}
	mustAgentTest(t, tx.Rollback(ctx))

	var transactionCount, refCount int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE id=$1`, transactionID).Scan(&transactionCount))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_turn_reference WHERE ref_key=$1`, refs[0].Ref).Scan(&refCount))
	if transactionCount != 0 || refCount != 0 {
		t.Fatalf("rollback left transaction=%d ref=%d", transactionCount, refCount)
	}
}
