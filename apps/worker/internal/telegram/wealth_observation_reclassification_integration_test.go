package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWealthObservationCanBeReclassifiedAsAssetPurchase(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var household, userID, accountID, wealthID, imageSource, documentID, observationID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("wealth reclass %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("wealth-reclass-%d@example.test", stamp)).Scan(&userID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, userID)
		return err
	}())
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, household, userID)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank Jago','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&accountID))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Bibit','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, household).Scan(&wealthID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("image-%d", stamp), []byte(fmt.Sprintf("image-%d", stamp))).Scan(&imageSource))
	var attachmentID string
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',1,1,1,$3) RETURNING id`, household, []byte(fmt.Sprintf("attachment-%d", stamp)), fmt.Sprintf("reclass-%d", stamp)).Scan(&attachmentID))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'WEALTH_OBSERVATION','NEEDS_REVIEW') RETURNING id`, household, imageSource, attachmentID).Scan(&documentID))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_observation(household_id,document_id,resolved_wealth_account_id,institution,account_hint,observed_value_idr) VALUES($1,$2,$3,'Bibit','Bibit',3000000) RETURNING id`, household, documentID, wealthID).Scan(&observationID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,wealth_observation_id,review_type,status) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN')`, household, observationID)
		return err
	}())
	var replySource string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprintf("reply-%d", stamp), []byte(fmt.Sprintf("reply-%d", stamp))).Scan(&replySource))

	update := telegramUpdate{}
	update.Message.From.ID, update.Message.Chat.ID, update.Message.MessageID = chatID, chatID, 1
	handled, err := NewProcessor(pool, nil).resolveNativeSpecialReview(ctx, replySource, household, update, "RECORD_ASSET_PURCHASE", map[string]any{
		"source_account_hint": "Bank Jago", "wealth_account_hint": "Bibit", "amount_idr": "3000000", "transaction_at": "2026-08-26T08:00:00+07:00",
	})
	must(err)
	if !handled {
		t.Fatal("wealth review was not handled")
	}
	var count int
	var typ, status, purpose, related, sourceAccount, observationStatus, reviewStatus, imageStatus, documentType, documentStatus string
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, household).Scan(&count))
	must(pool.QueryRow(ctx, `SELECT type,status,purpose,related_wealth_account_id::text,account_id::text FROM transaction WHERE household_id=$1`, household).Scan(&typ, &status, &purpose, &related, &sourceAccount))
	must(pool.QueryRow(ctx, `SELECT status FROM wealth_observation WHERE id=$1`, observationID).Scan(&observationStatus))
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE wealth_observation_id=$1`, observationID).Scan(&reviewStatus))
	must(pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, imageSource).Scan(&imageStatus))
	must(pool.QueryRow(ctx, `SELECT document_type,status FROM document WHERE id=$1`, documentID).Scan(&documentType, &documentStatus))
	if count != 1 || typ != "TRANSFER" || status != "CONFIRMED" || purpose != "ASSET_PURCHASE" || related != wealthID || sourceAccount != accountID || observationStatus != "DISMISSED" || reviewStatus != "RESOLVED" || imageStatus != "PROCESSED" || documentType != "TRANSACTION_HISTORY_SCREENSHOT" || documentStatus != "EXTRACTED" {
		t.Fatalf("count=%d type=%s status=%s purpose=%s account=%s wealth=%s observation=%s review=%s image=%s document=%s/%s", count, typ, status, purpose, sourceAccount, related, observationStatus, reviewStatus, imageStatus, documentType, documentStatus)
	}
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=(SELECT id FROM transaction WHERE household_id=$1) AND source_event_id=$2`, household, imageSource).Scan(&count))
	if count != 1 {
		t.Fatalf("image evidence=%d", count)
	}
	must(pool.QueryRow(ctx, `SELECT count(*) FROM wealth_snapshot WHERE household_id=$1`, household).Scan(&count))
	if count != 0 {
		t.Fatalf("snapshots=%d", count)
	}
}
