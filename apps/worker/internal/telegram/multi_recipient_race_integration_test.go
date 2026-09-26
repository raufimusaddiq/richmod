package telegram

// UIR-10 regression matrix: multi-recipient race. One review_item can be
// delivered to several household members. The first valid action must win; a
// second recipient acting on the same card must be answered as stale and must
// not double-mutate canonical state.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedTelegramReply(t *testing.T, pool *pgxpool.Pool, householdID, kind string, update telegramUpdate) string {
	t.Helper()
	raw, err := json.Marshal(update)
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

func TestMultiRecipientFirstActionWinsAndSecondIsStale(t *testing.T) {
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
	var householdID, firstUser, secondUser, categoryID, transactionID, itemID, reviewID string
	if err = pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("Multi recipient %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO \"user\"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id", fmt.Sprintf("multi-a-%d@example.test", stamp)).Scan(&firstUser); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO \"user\"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id", fmt.Sprintf("multi-b-%d@example.test", stamp)).Scan(&secondUser); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'OWNER')", householdID, firstUser, secondUser); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3),($4,$2,$5)", firstChat, householdID, firstUser, secondChat, secondUser); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id", householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',12000,now()) RETURNING id", householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnqueueReviewRequest(ctx, tx, transactionID, "UNKNOWN_MERCHANT", firstChat, 0, "Pilih kategori"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM review_item WHERE transaction_id=$1", transactionID).Scan(&itemID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM review_request WHERE transaction_id=$1", transactionID).Scan(&reviewID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE review_item SET decision=jsonb_set(decision,'{missingFacts}','[\"category\"]'::jsonb) WHERE id=$1", itemID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// The projection already inserted a recipient row per household identity and
	// a conversation row. Simulate the delivery job: mark the request/item OPEN
	// and bind each recipient's delivered message id.
	if _, err = pool.Exec(ctx, "UPDATE review_request SET status='OPEN' WHERE id=$1", reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE review_item SET status='OPEN' WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE review_conversation SET state='AWAITING_CATEGORY' WHERE review_request_id=$1", reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE review_request_recipient SET telegram_message_id=CASE telegram_chat_id WHEN $2 THEN 31 WHEN $3 THEN 32 END WHERE review_request_id=$1", reviewID, firstChat, secondChat); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(pool, boundReviewGateway{})
	if err = processor.Process(ctx, seedTelegramReply(t, pool, householdID, "TELEGRAM_CALLBACK", callbackUpdate(firstChat, 31, "review:cat:"+categoryID))); err != nil {
		t.Fatal(err)
	}
	var itemStatus, transactionStatus string
	if err = pool.QueryRow(ctx, "SELECT status FROM review_item WHERE id=$1", itemID).Scan(&itemStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT status FROM transaction WHERE id=$1", transactionID).Scan(&transactionStatus); err != nil {
		t.Fatal(err)
	}
	if itemStatus != "RESOLVED" || transactionStatus != "CONFIRMED" {
		t.Fatalf("first action must win: item=%s transaction=%s", itemStatus, transactionStatus)
	}
	if err = processor.Process(ctx, seedTelegramReply(t, pool, householdID, "TELEGRAM_CALLBACK", callbackUpdate(secondChat, 32, "review:cat:"+categoryID))); err != nil {
		t.Fatal(err)
	}
	var items, confirmed int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM review_item WHERE transaction_id=$1 AND status='RESOLVED'", transactionID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM transaction WHERE id=$1 AND status='CONFIRMED'", transactionID).Scan(&confirmed); err != nil {
		t.Fatal(err)
	}
	if items != 1 || confirmed != 1 {
		t.Fatalf("second action mutated canonical state: items=%d confirmed=%d", items, confirmed)
	}
}
