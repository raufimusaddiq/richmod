package telegram

// UIR-10 regression matrix: multi-recipient race for a bank-fact review. One
// UNKNOWN_BANK_TEMPLATE review_item can be delivered to several household
// members. The first valid reply must queue the shared completion job once; once
// the job resolves the item, a second recipient's reply on the same card must be
// answered as stale with no second job and no second mutation.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedTelegramRaw(t *testing.T, pool *pgxpool.Pool, householdID, kind string, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := pool.QueryRow(context.Background(), "INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),$4,'RECEIVED') RETURNING id", householdID, kind, fmt.Sprintf("%s-%d", kind, time.Now().UnixNano()), raw).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), "INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)", sourceID, string(raw)); err != nil {
		t.Fatal(err)
	}
	return sourceID
}

func seedTelegramReply(t *testing.T, pool *pgxpool.Pool, householdID string, chatID, messageID, replyTo int64, text string) string {
	t.Helper()
	return seedTelegramRaw(t, pool, householdID, "TELEGRAM_TEXT", map[string]any{
		"update_id": time.Now().UnixNano(),
		"message": map[string]any{
			"message_id":       messageID,
			"text":             text,
			"reply_to_message": map[string]any{"message_id": replyTo},
			"from":             map[string]any{"id": chatID},
			"chat":             map[string]any{"id": chatID},
		},
	})
}

func TestMultiRecipientBankRaceFirstReplyWinsSecondIsStale(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	firstChat, secondChat := stamp, stamp+1
	var householdID, firstUser, secondUser, accountID, listenerID, sourceEventID, itemID, reviewID string
	if err = pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("Multi bank %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-race-a-%d@example.test", stamp)).Scan(&firstUser); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-race-b-%d@example.test", stamp)).Scan(&secondUser); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'OWNER')", householdID, firstUser, secondUser); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3),($4,$2,$5)", firstChat, householdID, firstUser, secondChat, secondUser); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id", householdID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank','notifikasi@bank.test',$2,$3) RETURNING id", householdID, accountID, firstUser).Scan(&listenerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id", householdID, fmt.Sprintf("bank-race-%d", stamp), []byte("bank")).Scan(&sourceEventID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'IMAP','v1','{}'::jsonb,'VALID')", sourceEventID, listenerID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Mirror the bank-email producer: insert the OPEN item, then project it to
	// every eligible household Telegram identity through the shared projector.
	if err = tx.QueryRow(ctx, "INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN','{}'::jsonb) RETURNING id", householdID, sourceEventID).Scan(&itemID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = ProjectReviewItem(ctx, tx, householdID, itemID, 0, "", firstChat); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("project bank review: %v", err)
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM review_request WHERE review_item_id=$1", itemID).Scan(&reviewID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE review_request SET status='EXPIRED',expires_at=now()-interval '1 second' WHERE id=$1", reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE review_request_recipient SET telegram_message_id=CASE telegram_chat_id WHEN $2 THEN 31 WHEN $3 THEN 32 END WHERE review_request_id=$1", reviewID, firstChat, secondChat); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(pool, boundReviewGateway{})
	reply := seedTelegramReply(t, pool, householdID, firstChat, 41, 31, "54000 2026-09-23T13:45:00+07:00")
	if err = processor.Process(ctx, reply); err != nil {
		t.Fatal(err)
	}
	var renewed bool
	if err = pool.QueryRow(ctx, "SELECT status='OPEN' AND expires_at>now() FROM review_request WHERE id=$1", reviewID).Scan(&renewed); err != nil || !renewed {
		t.Fatalf("expired projection did not renew while canonical bank review remains open: %v %v", renewed, err)
	}
	var queued int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1", itemID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	var itemStatus string
	if err = pool.QueryRow(ctx, "SELECT status FROM review_item WHERE id=$1", itemID).Scan(&itemStatus); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || itemStatus != "OPEN" {
		t.Fatalf("first reply must queue exactly one job and leave the item open: jobs=%d item=%s", queued, itemStatus)
	}
	// The queued job resolves the item out of band; simulate that canonical outcome.
	if _, err = pool.Exec(ctx, "UPDATE review_item SET status='RESOLVED',resolved_at=now() WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	reply2 := seedTelegramReply(t, pool, householdID, secondChat, 42, 32, "54000 2026-09-23T13:45:00+07:00")
	if err = processor.Process(ctx, reply2); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1", itemID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	var resolved int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM review_item WHERE id=$1 AND status='RESOLVED'", itemID).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	var txns int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM transaction WHERE household_id=$1", householdID).Scan(&txns); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || resolved != 1 || txns != 0 {
		t.Fatalf("second action mutated canonical state: jobs=%d resolved=%d transactions=%d", queued, resolved, txns)
	}
}

func TestUnlinkedBankReviewBindsAccountThenCompletesThroughExistingJob(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	chat := stamp
	var household, user, account, listener, source, item, request string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Unlinked bank %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("unlinked-bank-%d@example.test", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chat, household, user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, household).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Bank','unlinked@bank.test',$2) RETURNING id`, household, user).Scan(&listener); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprintf("unlinked-bank-%d", stamp), []byte("bank")).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'IMAP','v1','{}'::jsonb,'VALID')`, source, listener); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN','{}'::jsonb) RETURNING id`, household, source).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if err := ProjectReviewItem(ctx, tx, household, item, 0, "", chat); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM review_request WHERE review_item_id=$1`, item).Scan(&request); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE review_request_recipient SET telegram_message_id=31 WHERE review_request_id=$1`, request); err != nil {
		t.Fatal(err)
	}
	p := NewProcessor(pool, boundReviewGateway{})
	invalid := seedTelegramReply(t, pool, household, chat, 40, 31, "-54000 2026-09-23T13:45:00+07:00")
	if err := p.Process(ctx, invalid); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1`, item).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("signed amount queued %d completion jobs", jobs)
	}
	reply := seedTelegramReply(t, pool, household, chat, 41, 31, "54000 2026-09-23T13:45:00+07:00")
	if err := p.Process(ctx, reply); err != nil {
		t.Fatal(err)
	}
	var pendingAmount, pendingAt string
	if err := pool.QueryRow(ctx, `SELECT context_json#>>'{bank_pending,amount_idr}',context_json#>>'{bank_pending,transaction_at}' FROM review_conversation WHERE review_request_id=$1`, request).Scan(&pendingAmount, &pendingAt); err != nil {
		t.Fatal(err)
	}
	if pendingAmount != "54000" || pendingAt != "2026-09-23T13:45:00+07:00" {
		t.Fatalf("pending bank facts lost: %s %s", pendingAmount, pendingAt)
	}
	if _, err := pool.Exec(ctx, `UPDATE review_request_recipient SET telegram_message_id=53 WHERE review_request_id=$1`, request); err != nil {
		t.Fatal(err)
	}
	callback := callbackUpdate(chat, 53, "review:bank:"+account)
	callbackSource := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": callback.CallbackQuery})
	if err := p.Process(ctx, callbackSource); err != nil {
		t.Fatal(err)
	}
	var linked string
	if err := pool.QueryRow(ctx, `SELECT account_id::text FROM bank_email_listener WHERE id=$1`, listener).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1`, item).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if linked != account || jobs != 1 {
		t.Fatalf("bank completion did not resume: linked=%s jobs=%d", linked, jobs)
	}
}
