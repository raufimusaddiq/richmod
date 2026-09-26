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

func callbackUpdate(chatID, messageID int64, data string) telegramUpdate {
	update := telegramUpdate{}
	update.Message.Chat.ID = chatID
	update.Message.From.ID = chatID
	update.Message.MessageID = messageID
	update.CallbackQuery = &struct {
		ID   string `json:"id"`
		Data string `json:"data"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Message struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	}{ID: "cb", Data: data}
	update.CallbackQuery.From.ID = chatID
	update.CallbackQuery.Message.Chat.ID = chatID
	update.CallbackQuery.Message.MessageID = messageID
	return update
}

func TestStaleReviewCallbackPersistsReply(t *testing.T) {
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
	var householdID, sourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Stale callback %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("stale-%d", stamp), []byte(fmt.Sprintf("stale-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := finishStaleReviewCallback(ctx, tx, sourceID, callbackUpdate(stamp, 42, "review:ignore")); err != nil {
		t.Fatal(err)
	}
	var status, message string
	if err := pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, sourceID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'reply_to_message_id'='42'`, fmt.Sprint(stamp)).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSED" || !strings.Contains(message, "sudah selesai") {
		t.Fatalf("stale callback status=%s reply=%q", status, message)
	}
}

// A TRANSFER_CLASSIFICATION review on an expense must offer the transfer chooser
// from a bound reply, and the chooser buttons must resolve it without leaving
// Telegram.
func TestTransferReviewOffersChooserAndCompletes(t *testing.T) {
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
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Transfer review %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("transfer-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'UNCLASSIFIED','NEEDS_REVIEW',100000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","decisionClass":"EVIDENCE_GAP","missingFacts":["transfer_relationship"],"boundedChoices":[],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,61)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 62, "text": "cek transfer ini", "reply_to_message": map[string]any{"message_id": 61}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("transfer-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var markupRaw string
	must(pool.QueryRow(ctx, `SELECT COALESCE(payload_json->>'reply_markup','') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND created_at > now() - interval '1 minute' ORDER BY created_at DESC LIMIT 1`).Scan(&markupRaw))
	if markupRaw == "" {
		t.Fatalf("transfer review did not offer a chooser markup: %q", markupRaw)
	}
	for _, want := range []string{"review:expense", "review:own", "review:household", "review:asset", "review:investment", "review:ignore"} {
		if !strings.Contains(markupRaw, want) {
			t.Fatalf("transfer chooser missing %s: %q", want, markupRaw)
		}
	}
	// Now click "rekening sendiri" and expect the review to resolve as a transfer.
	cbUpdate := callbackUpdate(chatID, 61, "review:own")
	cbRaw, _ := json.Marshal(cbUpdate)
	var cbSourceID string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("transfer-cb-%d", stamp), cbRaw).Scan(&cbSourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, cbSourceID, cbRaw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, cbSourceID); err != nil {
		t.Fatal(err)
	}
	var transactionType, transactionStatus string
	must(pool.QueryRow(ctx, `SELECT type,status FROM transaction WHERE id=$1`, transactionID).Scan(&transactionType, &transactionStatus))
	if transactionType != "TRANSFER" || transactionStatus != "CONFIRMED" {
		t.Fatalf("transfer callback did not resolve the review: type=%s status=%s", transactionType, transactionStatus)
	}
}

// A POSSIBLE_DUPLICATE review must offer its candidate chooser inside Telegram
// and complete the merge from a callback, without a Web detour.
func TestDuplicateReviewOffersChooserAndCompletes(t *testing.T) {
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
	var householdID, userID, transactionID, targetID, reviewID, itemID, sourceID string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Duplicate review %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("dup-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',57500,'IDR',$2) RETURNING id`, householdID, at).Scan(&transactionID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,'IDR',$2,now()) RETURNING id`, householdID, at.Add(-30*time.Minute)).Scan(&targetID))
	decision := `{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","decisionClass":"DUPLICATE_AMBIGUITY","missingFacts":["duplicate_relationship"],"boundedChoices":[],"allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'POSSIBLE_DUPLICATE',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,71)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 72, "text": "cek duplikat", "reply_to_message": map[string]any{"message_id": 71}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("dup-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var candidatesJSON string
	must(pool.QueryRow(ctx, `SELECT COALESCE(context_json->>'duplicate_candidates','') FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&candidatesJSON))
	if candidatesJSON == "" {
		t.Fatal("duplicate reply did not record candidate IDs")
	}
	cbUpdate := callbackUpdate(chatID, 71, "review:dup:merge:0")
	cbRaw, _ := json.Marshal(cbUpdate)
	var cbSourceID string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("dup-cb-%d", stamp), cbRaw).Scan(&cbSourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, cbSourceID, cbRaw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, cbSourceID); err != nil {
		t.Fatal(err)
	}
	var sourceStatus string
	must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&sourceStatus))
	if sourceStatus != "VOIDED" {
		t.Fatalf("duplicate merge did not void the source transaction: %s", sourceStatus)
	}
}

// An expense transfer review can only become an expense or an asset purchase, so
// the chooser must not offer the own/household account classifications that the
// canonical classifier rejects for an expense.
func TestExpenseTransferChooserOmitsInvalidClassifications(t *testing.T) {
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
	var householdID, userID, transactionID, reviewID, itemID string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Expense transfer %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("expense-transfer-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',100000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","decisionClass":"EVIDENCE_GAP","missingFacts":["transfer_relationship"],"boundedChoices":[],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	markup := reviewActionMarkupPage(ctx, tx, reviewID, "TRANSFER_CLASSIFICATION", 0)
	encoded, _ := json.Marshal(markup)
	rendered := string(encoded)
	if strings.Contains(rendered, "review:own") || strings.Contains(rendered, "review:household") {
		t.Fatalf("expense transfer chooser offered an invalid classification: %s", rendered)
	}
	for _, want := range []string{"review:expense", "review:asset"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expense transfer chooser missing %s: %s", want, rendered)
		}
	}
}
