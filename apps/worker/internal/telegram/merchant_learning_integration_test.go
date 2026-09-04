package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTelegramMerchantLearningUsesSeparateExplicitReply(t *testing.T) {
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
	var householdID, userID, categoryID, merchantID, transactionID, reviewID, confirmSourceID, rememberSourceID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Telegram merchant %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Telegram Owner','unused') RETURNING id`, fmt.Sprintf("telegram-merchant-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAMELLA DUA') RETURNING id`, householdID).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',55199,now(),$2) RETURNING id`, householdID, merchantID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO review_request(household_id,transaction_id,review_type,telegram_chat_id,telegram_message_id,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY',$3,17,'OPEN') RETURNING id`, householdID, transactionID, stamp).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, reviewID); err != nil {
		t.Fatal(err)
	}
	seedReply := func(external string) string {
		t.Helper()
		var sourceID string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, external, []byte(external)).Scan(&sourceID); err != nil {
			t.Fatal(err)
		}
		return sourceID
	}
	confirmSourceID = seedReply(fmt.Sprintf("confirm-%d", stamp))
	rememberSourceID = seedReply(fmt.Sprintf("remember-%d", stamp))
	update := telegramUpdate{}
	update.Message.MessageID = 22
	update.Message.From.ID = stamp
	update.Message.Chat.ID = stamp
	processor := NewProcessor(pool, nil)
	if err := processor.resolveReview(ctx, confirmSourceID, householdID, reviewID, transactionID, categoryID, update, reviewExtraction{Confidence: 1}); err != nil {
		t.Fatal(err)
	}
	var transactionStatus, reviewStatus, conversationState string
	var aliases int
	if err := pool.QueryRow(ctx, `SELECT t.status,r.status,c.state,(SELECT count(*) FROM merchant_alias WHERE household_id=$1) FROM transaction t JOIN review_request r ON r.transaction_id=t.id JOIN review_conversation c ON c.review_request_id=r.id WHERE t.id=$2`, householdID, transactionID).Scan(&transactionStatus, &reviewStatus, &conversationState, &aliases); err != nil {
		t.Fatal(err)
	}
	if transactionStatus != "CONFIRMED" || reviewStatus != "OPEN" || conversationState != "AWAITING_CONFIRMATION" || aliases != 0 {
		t.Fatalf("transaction=%s review=%s conversation=%s aliases=%d", transactionStatus, reviewStatus, conversationState, aliases)
	}
	update.Message.MessageID = 24
	update.Message.Text = "ingat merchant"
	if err := processor.rememberMerchantReply(ctx, rememberSourceID, householdID, reviewID, transactionID, update); err != nil {
		t.Fatal(err)
	}
	var autoApply bool
	if err := pool.QueryRow(ctx, `SELECT auto_apply FROM merchant_alias WHERE household_id=$1 AND normalized_merchant_id=$2`, householdID, merchantID).Scan(&autoApply); err != nil || !autoApply {
		t.Fatalf("auto apply=%t err=%v", autoApply, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, reviewID).Scan(&reviewStatus); err != nil || reviewStatus != "RESOLVED" {
		t.Fatalf("review=%s err=%v", reviewStatus, err)
	}
}
