package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	stamp := time.Now().UnixNano()
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
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "transfer")
	_, err := f.pool.Exec(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','FULL_LEDGER')`, f.householdID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)
	result, synthesize, err := p.agentRecordTransfer(ctx, f.state, gateway.ToolCall{CallID: "call-transfer", Name: "record_transfer"}, map[string]any{
		"amount_idr": "83000", "source_account_hint": "Jago", "destination_wealth_account_hint": nil,
		"purpose": "INTERNAL_TRANSFER", "date_reference": "TODAY", "explicit_date": nil, "local_time": "12:30", "description": "pindah rekening",
	})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "CONFIRMED" || result.Mutation["action"] != "TRANSFER_RECORDED" {
		t.Fatalf("unexpected transfer result: synthesize=%v result=%+v", synthesize, result)
	}
	var count int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND type='TRANSFER' AND status='CONFIRMED' AND amount=83000`, f.householdID).Scan(&count))
	if count != 1 {
		t.Fatalf("confirmed transfer count=%d, want 1", count)
	}
	assertNoDirectTelegramReplyJob(t, ctx, f)
}

func TestAgentSalaryChoiceReturnsStructuredResultWithoutCannedReply(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "salary")
	var transactionID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'INCOME','CONFIRMED',16000000,'IDR',now(),'Acme',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	_, err := f.pool.Exec(ctx, `INSERT INTO salary_pending_choice(household_id,telegram_user_id,telegram_chat_id,transaction_id,employer,payroll_period,pay_date,status) VALUES($1,$2,$3,$4,'Acme','2026-09-01','2026-09-01','PENDING')`, f.householdID, f.chatID, f.chatID, transactionID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)
	result, synthesize, err := p.agentResolveSalaryChoice(ctx, f.state, gateway.ToolCall{CallID: "call-salary", Name: "resolve_salary_choice"}, map[string]any{"choice": "ORDINARY"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "ORDINARY" || result.Mutation["action"] != "SALARY_CHOICE_RESOLVED" {
		t.Fatalf("unexpected salary result: synthesize=%v result=%+v", synthesize, result)
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM salary_pending_choice WHERE transaction_id=$1`, transactionID).Scan(&status))
	if status != "ORDINARY" {
		t.Fatalf("salary pending status=%s, want ORDINARY", status)
	}
	assertNoDirectTelegramReplyJob(t, ctx, f)
}

func TestAgentReviewConfirmationReturnsStructuredResultWithoutCannedReply(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review")
	var categoryID, transactionID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',70000,'IDR',now(),'makan',$2) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN',$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,99)`, reviewID, f.chatID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)
	binding, _, count, bindErr := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, bindErr)
	if binding == nil || count != 1 {
		t.Fatalf("review binding=%#v count=%d; want one bound review", binding, count)
	}
	f.state.ReviewBinding = binding
	f.state.ReviewBindingCount = count
	result, synthesize, err := p.agentResolveReview(ctx, f.state, gateway.ToolCall{CallID: "call-review", Name: "resolve_review"}, map[string]any{"action": "CONFIRM", "category_slug": "dining"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "RESOLVED" || result.Mutation["action"] != "REVIEW_CONFIRMED" {
		t.Fatalf("unexpected review result: synthesize=%v result=%+v", synthesize, result)
	}
	var status, gotCategory string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,category_id::text FROM transaction WHERE id=$1`, transactionID).Scan(&status, &gotCategory))
	if status != "CONFIRMED" || gotCategory != categoryID {
		t.Fatalf("reviewed transaction=%s/%s want CONFIRMED/%s", status, gotCategory, categoryID)
	}
	assertNoDirectTelegramReplyJob(t, ctx, f)
}
