package bankemail

// UISC-01 / acceptance A1, A4: a Cloudflare-origin BANK_EMAIL review is owned by
// its household before projection, so an actionable review must reach the
// household's eligible Telegram identity without any Telegram chat payload on
// the source event. Source provenance is evidence only; it never decides who may
// receive the review.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBankEmailReviewProjectsToHouseholdWithoutTelegramSource(t *testing.T) {
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
	var household, user, source string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank projection %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-proj-%d@test.invalid", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	chat := stamp
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chat, household, user); err != nil {
		t.Fatal(err)
	}
	// Cloudflare-origin bank email: a household-owned source event with no
	// source_event_payload message/chat object.
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprintf("bank-proj-event-%d", stamp), []byte("bank-proj")).Scan(&source); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}
	for attempt := 0; attempt < 2; attempt++ {
		if err = processor.reviewIncompleteExtraction(ctx, household, source, ToolSchemaVersion, "UNKNOWN_BANK_TEMPLATE", partialDecision(household, source, Extraction{}, "UNKNOWN_BANK_TEMPLATE", []string{"category"}, "test")); err != nil {
			t.Fatalf("project attempt %d: %v", attempt, err)
		}
	}
	var requests, recipients, sends int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE source_event_id=$1)`, source).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_request_recipient WHERE telegram_chat_id=$1 AND review_request_id IN (SELECT id FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE source_event_id=$2))`, chat, source).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE source_event_id=$1))`, source).Scan(&sends); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || recipients != 1 || sends != 1 {
		t.Fatalf("projection requests=%d recipients=%d sends=%d; want 1/1/1", requests, recipients, sends)
	}
}
