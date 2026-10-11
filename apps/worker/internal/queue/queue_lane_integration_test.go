package queue

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestClassifyLaneMatchesDatabase pins Go's lane table to the database
// enforce_job_lane trigger, which decides the lane a job is actually claimed
// from. They drifted once (00048), silently moving jobs off their intended lane.
func TestClassifyLaneMatchesDatabase(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, jobType := range []string{
		"PROCESS_TELEGRAM_CALLBACK", "PROCESS_TELEGRAM_TEXT", "PROCESS_TELEGRAM_REVIEW_TEXT",
		"SEND_TELEGRAM_MESSAGE", "EDIT_TELEGRAM_MESSAGE", "RETIRE_TELEGRAM_REVIEW_CARD",
		"FETCH_TELEGRAM_IMAGE", "FINALIZE_TELEGRAM_MEDIA_GROUP", "COMPLETE_BANK_REVIEW",
		"PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL", "PROCESS_FINANCIAL_EMAIL_PREVIEW",
		"PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT",
		"GENERATE_INSIGHT", "GENERATE_CYCLE_RESIDUAL_REVIEW",
	} {
		var lane string
		if err := tx.QueryRow(ctx, `INSERT INTO job(type,payload_json,run_after) VALUES($1,'{}'::jsonb,now()+interval '1 day') RETURNING lane`, jobType).Scan(&lane); err != nil {
			t.Fatalf("%s: %v", jobType, err)
		}
		if want := classifyLane(jobType); lane != want {
			t.Errorf("%s: database lane %s, Go lane %s", jobType, lane, want)
		}
	}
}
