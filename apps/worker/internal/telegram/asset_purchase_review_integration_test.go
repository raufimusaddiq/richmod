package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTelegramReviewClassifiesAssetPurchase(t *testing.T) {
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
	stamp, chatID := time.Now().UnixNano(), time.Now().UnixNano()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	var household, userID, wealthID, bankSource, transactionID, reviewID, replySource string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("telegram asset %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("telegram-asset-%d@example.test", stamp)).Scan(&userID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, userID)
		return err
	}())
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, household, userID)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Emas','ASSET','GOLD','OTHER') RETURNING id`, household).Scan(&wealthID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("bank-%d", stamp), []byte(fmt.Sprintf("bank-%d", stamp))).Scan(&bankSource))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name) VALUES($1,'UNCLASSIFIED','NEEDS_REVIEW',1000000,now(),'Toko Emas') RETURNING id`, household).Scan(&transactionID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, transactionID, bankSource)
		return err
	}())
	var itemID string
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN') RETURNING id`, household, transactionID).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION','OPEN',$4) RETURNING id`, household, itemID, transactionID, chatID).Scan(&reviewID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprintf("reply-%d", stamp), []byte(fmt.Sprintf("reply-%d", stamp))).Scan(&replySource))
	update := telegramUpdate{}
	update.Message.From.ID, update.Message.Chat.ID, update.Message.MessageID, update.Message.Text = chatID, chatID, 1, "Emas"
	must(NewProcessor(pool, nil).resolveTransferReview(ctx, replySource, household, reviewID, transactionID, update, "TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "ok", ""))
	var typ, status, purpose, related string
	must(pool.QueryRow(ctx, `SELECT type,status,purpose,related_wealth_account_id::text FROM transaction WHERE id=$1`, transactionID).Scan(&typ, &status, &purpose, &related))
	if typ != "TRANSFER" || status != "CONFIRMED" || purpose != "ASSET_PURCHASE" || related != wealthID {
		t.Fatalf("transaction=%s/%s/%s/%s", typ, status, purpose, related)
	}
}
