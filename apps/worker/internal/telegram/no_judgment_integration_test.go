package telegram

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// ADR-052: the worker runs the Telegram agent with no judgment plane. A stated
// expense must still reach record_transaction and be persisted, whatever a route
// classifier would have said about the message.
func TestTelegramRecordsExpenseWithoutAJudgmentPlane(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "no-judgment")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food & Drink','food-drink')`, f.householdID)
	mustAgentTest(t, err)
	f.update.Message.MessageID = 79
	f.update.Message.Text = "jam 3 sore"
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)

	agent := &ir04AgentGateway{amount: "20000", merchant: "Cilok", category: "food-drink", dateRef: "YESTERDAY", localTime: "15:00"}
	p := NewProcessor(f.pool, agent)
	p.now = func() time.Time { return time.Date(2026, 10, 12, 8, 0, 0, 0, jakartaLocation()) }
	if err = p.ProcessAgent(ctx, f.sourceID); err != nil {
		t.Fatal(err)
	}
	var status string
	var amount int64
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,t.amount::bigint FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, f.sourceID).Scan(&status, &amount))
	if agent.calls != 1 || status != "CONFIRMED" || amount != 20000 {
		t.Fatalf("calls=%d status=%s amount=%d, want the stated expense confirmed", agent.calls, status, amount)
	}
}
