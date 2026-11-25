package telegram

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
