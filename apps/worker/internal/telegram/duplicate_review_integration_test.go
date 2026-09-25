package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDuplicateReview creates one open POSSIBLE_DUPLICATE review bound to a
// Telegram message, plus the confirmed transaction it may merge into.
func seedDuplicateReview(t *testing.T, pool *pgxpool.Pool, stamp int64) (household, userID string, chatID int64, sourceID, duplicateID, targetID, reviewID, dupItemID string) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	chatID = stamp
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("tg duplicate %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("tg-duplicate-%d@example.test", stamp)).Scan(&userID))
	_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1::bigint,$2,$3)`, stamp, household, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Utama','BANK','FULL_LEDGER')`, household)
	if err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("dup-%d", stamp), []byte(fmt.Sprintf("dup-%d", stamp))).Scan(&sourceID))
	at := time.Now()
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name) VALUES($1,'EXPENSE','NEEDS_REVIEW',57500,'IDR',$2,'Indomaret') RETURNING id`, household, at).Scan(&duplicateID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,'IDR',$2,'Indomaret',now()) RETURNING id`, household, at.Add(-30*time.Minute)).Scan(&targetID))
	_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'RECEIPT_IMAGE','{}')`, duplicateID, sourceID)
	must(err)
	decision := fmt.Sprintf(`{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"],"missingFacts":["duplicate_relationship"]}`)

	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb) RETURNING id`, household, duplicateID, decision).Scan(&dupItemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'POSSIBLE_DUPLICATE','OPEN',$4::bigint) RETURNING id`, household, dupItemID, duplicateID, chatID).Scan(&reviewID))
	_, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID)
	must(err)
	return household, userID, chatID, sourceID, duplicateID, targetID, reviewID, dupItemID
}

func TestTelegramDuplicateMergeUsesSharedOperation(t *testing.T) {
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
	household, _, chatID, _, duplicateID, targetID, reviewID, dupItemID := seedDuplicateReview(t, pool, stamp)
	if _, err := pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,1)`, reviewID, chatID); err != nil {
		t.Fatal(err)
	}
	var replySource string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprintf("cb-%d", stamp), []byte(fmt.Sprintf("cb-%d", stamp))).Scan(&replySource); err != nil {
		t.Fatal(err)
	}
	update := telegramUpdate{}
	update.Message.Chat.ID = chatID
	update.Message.From.ID = chatID
	update.Message.MessageID = 1
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
	}{ID: "cb", Data: "review:dup:merge:0"}
	update.CallbackQuery.From.ID = chatID
	update.CallbackQuery.Message.Chat.ID = chatID
	update.CallbackQuery.Message.MessageID = 1
	update.Message.Chat.ID = chatID
	update.Message.MessageID = 1
	update.Message.From.ID = chatID
	if _, err := pool.Exec(ctx, `UPDATE review_conversation SET context_json=jsonb_build_object('duplicate_candidates',jsonb_build_array($2::text)) WHERE review_request_id=$1`, reviewID, targetID); err != nil {
		t.Fatal(err)
	}
	if handled, err := NewProcessor(pool, nil).processReviewDetailCallback(ctx, replySource, household, update, "review:dup:merge:0"); !handled || err != nil {
		t.Fatalf("duplicate merge handled=%v err=%v", handled, err)
	}
	var sourceStatus, reviewStatus string
	var mergeStatus sql.NullString
	if err := pool.QueryRow(ctx, `SELECT (SELECT status FROM transaction WHERE id=$1),(SELECT status FROM review_item WHERE id=$2),(SELECT status FROM reconciliation_merge WHERE source_transaction_id=$1)`, duplicateID, dupItemID).Scan(&sourceStatus, &reviewStatus, &mergeStatus); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "VOIDED" || reviewStatus != "RESOLVED" || !mergeStatus.Valid || mergeStatus.String != "ACTIVE" {
		t.Fatalf("source=%s review=%s merge=%v", sourceStatus, reviewStatus, mergeStatus)
	}
	var targetStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, targetID).Scan(&targetStatus); err != nil || targetStatus != "CONFIRMED" {
		t.Fatalf("target=%s err=%v", targetStatus, err)
	}
}
