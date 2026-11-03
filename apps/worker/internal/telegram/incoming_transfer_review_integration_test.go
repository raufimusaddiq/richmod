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

// An incoming screenshot row ("dana masuk") is typed INCOME and parked as a
// transfer-classification review. Its card must offer only answers that apply to
// money coming in, and each of them must resolve the review from Telegram.
func TestIncomingRowReviewIsAnswerableFromTelegram(t *testing.T) {
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

	cases := []struct {
		callback   string
		wantType   string
		wantStatus string
	}{
		{"review:income", "INCOME", "CONFIRMED"},
		{"review:own", "TRANSFER", "CONFIRMED"},
		{"review:household", "TRANSFER", "CONFIRMED"},
		{"review:ignore", "INCOME", "VOIDED"},
		// Outgoing semantics never apply to money coming in: the review stays open.
		{"review:expense", "INCOME", "NEEDS_REVIEW"},
		{"review:investment", "INCOME", "NEEDS_REVIEW"},
	}
	for _, tc := range cases {
		t.Run(tc.callback, func(t *testing.T) {
			stamp := time.Now().UnixNano()
			chatID := stamp
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var householdID, userID, transactionID, itemID, reviewID, cbSourceID string
			must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Incoming review %d", stamp)).Scan(&householdID))
			must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("incoming-%d@example.test", stamp)).Scan(&userID))
			_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID)
			must(err)
			must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',2097373,'IDR',now()) RETURNING id`, householdID).Scan(&transactionID))
			decision := `{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","decisionClass":"HUMAN_POLICY_CHOICE","missingFacts":["transfer_relationship"],"boundedChoices":[],"allowedActions":["CLASSIFY_TRANSFER","OWN_ACCOUNT","HOUSEHOLD_ACCOUNT","IGNORE"]}`
			must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN',$3::jsonb) RETURNING id`, householdID, transactionID, decision).Scan(&itemID))
			must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION',$4,'OPEN') RETURNING id`, householdID, itemID, transactionID, chatID).Scan(&reviewID))
			_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,71)`, reviewID, chatID)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID)
			must(err)

			// The card offers the income answers, never the outgoing ones.
			tx, err := pool.Begin(ctx)
			must(err)
			markup := reviewActionMarkupPage(ctx, tx, reviewID, "TRANSFER_CLASSIFICATION", 0)
			must(tx.Rollback(ctx))
			encoded, _ := json.Marshal(markup)
			for _, want := range []string{"review:income", "review:own", "review:household", "review:ignore"} {
				if !strings.Contains(string(encoded), want) {
					t.Fatalf("an incoming row must offer %s: %s", want, encoded)
				}
			}
			for _, banned := range []string{"review:expense", "review:asset", "review:investment"} {
				if strings.Contains(string(encoded), banned) {
					t.Fatalf("an incoming row must not offer %s: %s", banned, encoded)
				}
			}

			raw, _ := json.Marshal(callbackUpdate(chatID, 71, tc.callback))
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("incoming-cb-%d", stamp), raw).Scan(&cbSourceID))
			_, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, cbSourceID, raw)
			must(err)
			must(NewProcessor(pool, boundReviewGateway{}).Process(ctx, cbSourceID))

			var gotType, gotStatus, itemStatus string
			must(pool.QueryRow(ctx, `SELECT t.type,t.status,ri.status FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE t.id=$1`, transactionID).Scan(&gotType, &gotStatus, &itemStatus))
			if gotType != tc.wantType || gotStatus != tc.wantStatus {
				t.Fatalf("%s: type=%s status=%s, want %s/%s", tc.callback, gotType, gotStatus, tc.wantType, tc.wantStatus)
			}
			if resolved := tc.wantStatus != "NEEDS_REVIEW"; resolved != (itemStatus == "RESOLVED") {
				t.Fatalf("%s: review item status=%s for transaction %s", tc.callback, itemStatus, gotStatus)
			}
			if tc.callback == "review:ignore" {
				// Abaikan on a transfer card must not claim an income-specific reason.
				var reason, reviewType string
				must(pool.QueryRow(ctx, `SELECT after_json->>'reason',after_json->>'review_type' FROM audit_log WHERE entity_id=$1 AND action='REJECT_REVIEW' AND actor_type='TELEGRAM'`, transactionID).Scan(&reason, &reviewType))
				if reason != "telegram_ignore" || reviewType != "TRANSFER_CLASSIFICATION" {
					t.Fatalf("ignore audit reason=%s review_type=%s", reason, reviewType)
				}
			}
		})
	}
}
