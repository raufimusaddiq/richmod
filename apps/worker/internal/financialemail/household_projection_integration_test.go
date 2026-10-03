package financialemail

// An observation-scoped FINANCIAL_EMAIL review
// projects to the household's eligible Telegram identity even when the source
// event carries no Telegram chat payload.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFinancialEmailReviewProjectsToHouseholdWithoutTelegramSource(t *testing.T) {
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
	household, user, _, _, source := seedFinancialEmail(t, ctx, pool, stamp, "ACTIVE")
	chat := stamp
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chat, household, user); err != nil {
		t.Fatal(err)
	}
	var observation string
	if err = pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,1,'CASH_MOVEMENT','{}'::jsonb,'PENDING') RETURNING id`, household, source).Scan(&observation); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(pool, nil)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for attempt := 0; attempt < 2; attempt++ {
		if err = processor.insertReviewDecision(ctx, tx, household, observation, "FINANCIAL_EMAIL_RESOLUTION"); err != nil {
			t.Fatalf("project attempt %d: %v", attempt, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var requests, recipients, sends int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE financial_email_observation_id=$1)`, observation).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_request_recipient WHERE telegram_chat_id=$1 AND review_request_id IN (SELECT id FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE financial_email_observation_id=$2))`, chat, observation).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE financial_email_observation_id=$1))`, observation).Scan(&sends); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || recipients != 1 || sends != 1 {
		t.Fatalf("projection requests=%d recipients=%d sends=%d; want 1/1/1", requests, recipients, sends)
	}
}
