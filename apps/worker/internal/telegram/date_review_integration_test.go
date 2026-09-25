package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A date-only review must ask for the transaction date, persist it on the bound
// reply, and complete the review. Before the UIR-03 renderer the bound reply lane
// routed AWAITING_DETAIL to the description field, so the date was never stored.
func TestTelegramBoundDateReviewPersistsTransactionDate(t *testing.T) {
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
	chatID := stamp
	var householdID, userID, transactionID, reviewID, itemID, sourceID string
	mustQuery := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	mustQuery(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Date review %d", stamp)).Scan(&householdID))
	mustQuery(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("date-review-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	mustQuery(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',12000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"MISSING_TRANSACTION_DATE","decisionClass":"EVIDENCE_GAP","knownFacts":{},"missingFacts":["transaction_at"],"allowedActions":["SET_PAY_DATE","IGNORE"],"interactionMode":"SINGLE_FIELD"}`
	mustQuery(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'MISSING_TRANSACTION_DATE','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	mustQuery(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'MISSING_TRANSACTION_DATE',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,27)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DATE')`, reviewID); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 28, "text": "2026-09-20", "reply_to_message": map[string]any{"message_id": 27}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	mustQuery(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("date-review-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}

	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var at time.Time
	var sourceStatus, itemStatus, requestStatus, convState string
	mustQuery(pool.QueryRow(ctx, `SELECT t.transaction_at,s.processing_status,ri.status,rr.status,rc.state FROM transaction t JOIN source_event s ON s.id=$2 JOIN review_item ri ON ri.id=$3 JOIN review_request rr ON rr.id=$4 JOIN review_conversation rc ON rc.review_request_id=rr.id WHERE t.id=$1`, transactionID, sourceID, itemID, reviewID).Scan(&at, &sourceStatus, &itemStatus, &requestStatus, &convState))
	if got := at.In(jakartaLocation()).Format("2006-01-02"); got != "2026-09-20" {
		t.Fatalf("transaction_at=%s", got)
	}
	if itemStatus != "RESOLVED" || requestStatus != "RESOLVED" || convState != "RESOLVED" {
		t.Fatalf("item=%s request=%s conversation=%s", itemStatus, requestStatus, convState)
	}
	if sourceStatus != "PROCESSED" {
		t.Fatalf("source=%s", sourceStatus)
	}
	var txStatus string
	mustQuery(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&txStatus))
	if txStatus != "CONFIRMED" {
		t.Fatalf("transaction=%s, want CONFIRMED", txStatus)
	}
}

// An invalid date must re-prompt and stay in the date state so the next reply is
// still routed to the date parser.
func TestTelegramBoundDateReviewRepromptsOnInvalidDate(t *testing.T) {
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
	chatID := stamp
	var householdID, userID, transactionID, reviewID, itemID, sourceID string
	mustQuery := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	mustQuery(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Date reprompt %d", stamp)).Scan(&householdID))
	mustQuery(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("date-reprompt-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	mustQuery(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',12000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"MISSING_TRANSACTION_DATE","missingFacts":["transaction_at"]}`
	mustQuery(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'MISSING_TRANSACTION_DATE','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	mustQuery(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'MISSING_TRANSACTION_DATE',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,37)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DATE')`, reviewID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 38, "text": "20-09-2026", "reply_to_message": map[string]any{"message_id": 37}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	mustQuery(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("date-reprompt-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var convState, txStatus, itemStatus string
	mustQuery(pool.QueryRow(ctx, `SELECT rc.state,t.status,ri.status FROM review_conversation rc JOIN review_request rr ON rr.id=rc.review_request_id JOIN transaction t ON t.id=rr.transaction_id JOIN review_item ri ON ri.id=rr.review_item_id WHERE rr.id=$1`, reviewID).Scan(&convState, &txStatus, &itemStatus))
	if convState != "AWAITING_DATE" {
		t.Fatalf("conversation=%s, want AWAITING_DATE after invalid date", convState)
	}
	if txStatus != "NEEDS_REVIEW" || itemStatus != "OPEN" {
		t.Fatalf("transaction=%s item=%s after invalid date", txStatus, itemStatus)
	}
}
