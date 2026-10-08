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

// The category pager callback on a category-only review is already sitting on the
// chooser message, so it must re-render the chooser in place instead of pointing
// the user at the Review Inbox.
func TestCategoryOnlyReviewCategoryPagerRerenders(t *testing.T) {
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
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Chooser pager %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("cat-pager-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan')`, householdID); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',21000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"UNKNOWN_MERCHANT","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"boundedChoices":[],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'UNKNOWN_MERCHANT',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,77)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, reviewID); err != nil {
		t.Fatal(err)
	}
	update := telegramUpdate{}
	update.Message.Chat.ID = chatID
	update.Message.From.ID = chatID
	update.Message.MessageID = 77
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
	}{ID: "cb-pager", Data: "review:catpage:1"}
	update.CallbackQuery.From.ID = chatID
	update.CallbackQuery.Message.Chat.ID = chatID
	update.CallbackQuery.Message.MessageID = 77
	raw, _ := json.Marshal(update)
	var sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("cat-pager-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var editText string
	must(pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND payload_json->>'message_id'='77' AND created_at > now() - interval '1 minute' ORDER BY created_at DESC LIMIT 1`).Scan(&editText))
	if strings.Contains(editText, "Review Inbox") {
		t.Fatalf("pager callback still redirects to the Review Inbox: %q", editText)
	}
	if !strings.Contains(editText, "halaman 2") {
		t.Fatalf("pager callback did not render the requested page: %q", editText)
	}
}

// Clicking a category button on a category-only review must resolve the review,
// not get swallowed by a chooser re-render.
func TestCategoryOnlyReviewSelectionCompletesReview(t *testing.T) {
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
	var householdID, userID, categoryID, transactionID, reviewID, itemID string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Chooser select %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("cat-select-%d@example.test", stamp)).Scan(&userID))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, householdID).Scan(&categoryID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',21000,now()) RETURNING id`, householdID).Scan(&transactionID))
	decision := `{"version":1,"reasonCode":"UNKNOWN_MERCHANT","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"boundedChoices":[],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'UNKNOWN_MERCHANT',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,81)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, reviewID); err != nil {
		t.Fatal(err)
	}
	update := telegramUpdate{}
	update.Message.Chat.ID = chatID
	update.Message.From.ID = chatID
	update.Message.MessageID = 81
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
	}{ID: "cb-select", Data: "review:cat:" + categoryID}
	update.CallbackQuery.From.ID = chatID
	update.CallbackQuery.Message.Chat.ID = chatID
	update.CallbackQuery.Message.MessageID = 81
	raw, _ := json.Marshal(update)
	var sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("cat-select-%d", stamp), raw).Scan(&sourceID))
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var transactionStatus, categoryIDSet string
	must(pool.QueryRow(ctx, `SELECT status,COALESCE(category_id::text,'') FROM transaction WHERE id=$1`, transactionID).Scan(&transactionStatus, &categoryIDSet))
	if transactionStatus != "CONFIRMED" || categoryIDSet != categoryID {
		t.Fatalf("selection did not resolve the review: status=%s category=%s", transactionStatus, categoryIDSet)
	}
}
