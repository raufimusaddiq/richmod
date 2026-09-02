package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type boundReviewGateway struct{}

func (boundReviewGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("bound review reply must not call LLM")
}

type clearPurchaseGateway struct{}

func (clearPurchaseGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{Name: "record_transaction", Arguments: json.RawMessage(`{"type":"EXPENSE","amount_idr":"9000","merchant":"Indomaret","category_slug":"makanan-minuman","description":"Beli es krim","note":null,"date_reference":"TODAY","explicit_date":null,"local_time":null,"confidence":0.98,"category_confidence":0.88}`)}, gateway.Metadata{Model: "test"}, nil
}

func TestTelegramReplyToBoundMerchantReviewBypassesLLM(t *testing.T) {
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
	var householdID, userID, transactionID, reviewID, sourceID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bound review %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bound-review-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan')`, householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',9000,now()) RETURNING id`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO review_request(household_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,'UNKNOWN_MERCHANT',$3,'OPEN') RETURNING id`, householdID, transactionID, chatID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,17)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_MERCHANT')`, reviewID); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 18, "text": "Indomaret", "reply_to_message": map[string]any{"message_id": 17}, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("bound-review-%d", stamp), raw).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}

	if err = NewProcessor(pool, boundReviewGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var merchant, sourceStatus, conversationState string
	if err = pool.QueryRow(ctx, `SELECT m.normalized_name,s.processing_status,c.state FROM transaction t JOIN merchant m ON m.id=t.merchant_id JOIN source_event s ON s.id=$2 JOIN review_conversation c ON c.review_request_id=$3 WHERE t.id=$1`, transactionID, sourceID, reviewID).Scan(&merchant, &sourceStatus, &conversationState); err != nil {
		t.Fatal(err)
	}
	if merchant != "Indomaret" || sourceStatus != "PROCESSED" || conversationState != "AWAITING_CATEGORY" {
		t.Fatalf("merchant=%q source=%q review=%q", merchant, sourceStatus, conversationState)
	}
}

func TestClearPurchaseWithValidCategoryDoesNotCreateReview(t *testing.T) {
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
	var householdID, userID, categoryID, sourceID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Clear purchase %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("clear-purchase-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman') RETURNING id`, householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"update_id": stamp, "message": map[string]any{"message_id": 19, "text": "beli es krim indomaret 9k", "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("clear-purchase-%d", stamp), raw).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
		t.Fatal(err)
	}
	if err = NewProcessor(pool, clearPurchaseGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var status, merchant, description, gotCategoryID string
	var reviewCount int
	if err = pool.QueryRow(ctx, `SELECT status,COALESCE(counterparty_name,''),COALESCE(description,''),category_id::text,(SELECT count(*) FROM review_request WHERE transaction_id=t.id) FROM transaction t WHERE household_id=$1 ORDER BY created_at DESC LIMIT 1`, householdID).Scan(&status, &merchant, &description, &gotCategoryID, &reviewCount); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || merchant != "Indomaret" || description != "Beli es krim" || gotCategoryID != categoryID || reviewCount != 0 {
		t.Fatalf("status=%q merchant=%q description=%q category=%q reviews=%d", status, merchant, description, gotCategoryID, reviewCount)
	}
}
