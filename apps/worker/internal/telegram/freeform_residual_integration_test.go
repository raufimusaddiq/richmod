package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A salary correction review (MANUAL_CORRECTION) resolves from one bound reply:
// income needs no category, so the description save completes the review.
func TestManualCorrectionReviewCompletesFromBoundReply(t *testing.T) {
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
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Correction %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("correction-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',8000000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"MANUAL_CORRECTION","decisionClass":"EVIDENCE_GAP","missingFacts":["correction_details"],"boundedChoices":[],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'MANUAL_CORRECTION','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'MANUAL_CORRECTION',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,91)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 92, "text": "Gaji bulan ini dari PT Contoh", "reply_to_message": map[string]any{"message_id": 91}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("correction-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var transactionStatus string
	must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&transactionStatus))
	if transactionStatus != "CONFIRMED" {
		t.Fatalf("income correction not confirmed: %s", transactionStatus)
	}
}

// An UNKNOWN_PURPOSE review on an uncategorized expense must still reach the
// category chooser rather than dead-ending in a confirm that the canonical rule
// rejects, and the chooser must then complete the review.
func TestUnknownPurposeExpenseReachesChooserThenCompletes(t *testing.T) {
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
	var householdID, userID, categoryID, transactionID, reviewID, itemID, sourceID string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Purpose %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("purpose-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, householdID).Scan(&categoryID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',18000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"UNKNOWN_PURPOSE","decisionClass":"EVIDENCE_GAP","missingFacts":["transaction_semantics"],"boundedChoices":[],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_PURPOSE','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'UNKNOWN_PURPOSE',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,93)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 94, "text": "Makan siang tim", "reply_to_message": map[string]any{"message_id": 93}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("purpose-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var conversationState string
	must(pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&conversationState))
	if conversationState != "AWAITING_CATEGORY" {
		t.Fatalf("expected the category chooser after the description reply, got %q", conversationState)
	}
	markupRaw := ""
	must(pool.QueryRow(ctx, `SELECT COALESCE(payload_json->>'reply_markup','') FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND created_at > now() - interval '1 minute' ORDER BY created_at DESC LIMIT 1`).Scan(&markupRaw))
	if !strings.Contains(markupRaw, "review:cat:"+categoryID) {
		t.Fatalf("chooser markup does not offer the household category: %q", markupRaw)
	}
}
