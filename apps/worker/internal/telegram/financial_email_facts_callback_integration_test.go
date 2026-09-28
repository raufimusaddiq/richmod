package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestFinancialEmailFactsCallbackCompletesReview pins the Hermes blocker: the
// FINANCIAL_EMAIL_FACTS card's only button (review:ignore) must complete the
// review from the callback path, not just from the agent resolveNativeReview
// lane. Before the fix the tap fell through to the stale-action reply and left
// the review_item/review_request OPEN with the observation at REVIEW (SAVR-06).
func TestFinancialEmailFactsCallbackCompletesReview(t *testing.T) {
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
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	chatID := stamp
	var householdID, userID, sourceID, observationID, itemID, requestID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("facts callback %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("facts-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'FINANCIAL_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("facts-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,0,'CASH_MOVEMENT','{}'::jsonb,'REVIEW') RETURNING id`, householdID, sourceID).Scan(&observationID))
	decision := `{"version":1,"reasonCode":"FINANCIAL_EMAIL_FACTS","decisionClass":"EVIDENCE_GAP","interactionMode":"SINGLE_FIELD","knownFacts":{},"missingFacts":["evidence_support"],"allowedActions":["IGNORE"],"decisionSource":"DETERMINISTIC"}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,'FINANCIAL_EMAIL_FACTS','OPEN',$3::jsonb) RETURNING id`, householdID, observationID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'FINANCIAL_EMAIL_FACTS','OPEN') RETURNING id`, itemID, householdID).Scan(&requestID))
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,91)`, requestID, chatID)
	must(err)
	raw, err := json.Marshal(callbackUpdate(chatID, 91, "review:ignore"))
	must(err)
	var callbackSource string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("facts-cb-%d", stamp), raw).Scan(&callbackSource))
	_, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, callbackSource, string(raw))
	must(err)
	must(NewProcessor(pool, nil).Process(ctx, callbackSource))
	var itemStatus, requestStatus, observationStatus string
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&itemStatus))
	must(pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, requestID).Scan(&requestStatus))
	must(pool.QueryRow(ctx, `SELECT status FROM financial_email_observation WHERE id=$1`, observationID).Scan(&observationStatus))
	if itemStatus != "RESOLVED" || requestStatus != "RESOLVED" || observationStatus != "IGNORED" {
		t.Fatalf("the facts card must complete on review:ignore: item=%s request=%s observation=%s", itemStatus, requestStatus, observationStatus)
	}
	// The lane must settle the provider email's own source event, not the
	// Telegram callback event it was loaded from. Before the fix the CASE always
	// fell to ELSE and the email stayed NEEDS_REVIEW (SAVR-06, Hermes round 5).
	var emailStatus string
	must(pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, sourceID).Scan(&emailStatus))
	if emailStatus == "NEEDS_REVIEW" {
		t.Fatalf("the provider email event must settle on ignore, got %s", emailStatus)
	}
}
