package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type agentIntegrationFixture struct {
	pool        *pgxpool.Pool
	householdID string
	userID      string
	sourceID    string
	chatID      int64
	update      telegramUpdate
	state       *agentState
}

func newAgentIntegrationFixture(t *testing.T, label string) agentIntegrationFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	stamp := testsupport.UniqueStamp()
	chatID := stamp
	var householdID, userID, sourceID string
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("agent %s %d", label, stamp)).Scan(&householdID))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("agent-%s-%d@example.test", label, stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
	mustAgentTest(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID)
	mustAgentTest(t, err)
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("agent-%s-%d", label, stamp), []byte(fmt.Sprintf("agent-%s-%d", label, stamp))).Scan(&sourceID))
	update := telegramUpdate{}
	update.Message.From.ID = chatID
	update.Message.Chat.ID = chatID
	update.Message.MessageID = 1
	return agentIntegrationFixture{
		pool: pool, householdID: householdID, userID: userID, sourceID: sourceID, chatID: chatID, update: update,
		state: &agentState{SourceEventID: sourceID, HouseholdID: householdID, Update: update, Now: time.Now().In(jakartaLocation()), ModelPhases: 1},
	}
}

func (f agentIntegrationFixture) seedSourceEvent(ctx context.Context, t *testing.T, label string, payload []byte) string {
	t.Helper()
	var id string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, f.householdID, label, payload).Scan(&id))
	mustAgentTest(t, func() error {
		_, err := f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, id, payload)
		return err
	}())
	return id
}

func mustAgentTest(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertNoDirectTelegramReplyJob(t *testing.T, ctx context.Context, f agentIntegrationFixture) {
	t.Helper()
	var count int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1`, fmt.Sprint(f.chatID)).Scan(&count))
	if count != 0 {
		t.Fatalf("specialized agent mutation created %d direct Telegram reply job(s); response must come from LLM synthesis", count)
	}
}

func TestAgentRecordTransferReturnsStructuredResultWithoutCannedReply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "transfer")
	_, err := f.pool.Exec(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','FULL_LEDGER')`, f.householdID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)
	result, synthesize, err := p.agentRecordTransfer(ctx, f.state, gateway.ToolCall{CallID: "call-transfer", Name: "record_transfer"}, map[string]any{
		"amount_idr": "83000", "source_account_hint": "Jago", "destination_wealth_account_hint": nil,
		"date_reference": "TODAY", "date_provenance": "USER_STATED", "explicit_date": nil, "local_time": "12:30", "description": "pindah rekening",
	})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "CONFIRMED" || result.Mutation["action"] != "TRANSFER_RECORDED" {
		t.Fatalf("unexpected transfer result: synthesize=%v result=%+v", synthesize, result)
	}
	var count, autoConfirmed int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE auto_confirmed_at IS NOT NULL) FROM transaction WHERE household_id=$1 AND type='TRANSFER' AND status='CONFIRMED' AND amount=83000`, f.householdID).Scan(&count, &autoConfirmed))
	if count != 1 || autoConfirmed != 1 {
		t.Fatalf("confirmed transfer count=%d auto-confirmed=%d, want 1 each", count, autoConfirmed)
	}
	var audited int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='RECORD_TRANSFER_FROM_TELEGRAM' AND after_json->>'purpose'='INTERNAL_TRANSFER'`, f.householdID).Scan(&audited))
	if audited != 1 {
		t.Fatalf("transfer audit rows=%d, want 1", audited)
	}
	assertNoDirectTelegramReplyJob(t, ctx, f)
}
