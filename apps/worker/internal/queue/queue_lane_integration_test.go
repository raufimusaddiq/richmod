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

// TestClaimSharesALaneFairlyAcrossHouseholds runs the real claim statement
// against a session-local job table, so other tests' jobs cannot affect it.
// Household A has a backlog and just ran a job; household B's newer job must
// still be claimed before A's next one, and A's own jobs keep their order.
func TestClaimSharesALaneFairlyAcrossHouseholds(t *testing.T) {
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
	// pg_temp precedes public in the search path, so claimSQL reads this table.
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE job (LIKE public.job INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	const householdA, householdB = "00000000-0000-4000-8000-00000000000a", "00000000-0000-4000-8000-00000000000b"
	if _, err := tx.Exec(ctx, `INSERT INTO job(id,type,lane,household_id,status,payload_json,run_after,started_at,created_at) VALUES
		('10000000-0000-4000-8000-000000000001','PROCESS_DOCUMENT','BACKGROUND',$1,'SUCCEEDED','{}',now()-interval '2 hours',now()-interval '1 minute',now()-interval '2 hours'),
		('10000000-0000-4000-8000-000000000002','PROCESS_DOCUMENT','BACKGROUND',$1,'PENDING','{}',now()-interval '60 minutes',NULL,now()-interval '60 minutes'),
		('10000000-0000-4000-8000-000000000003','PROCESS_DOCUMENT','BACKGROUND',$1,'PENDING','{}',now()-interval '50 minutes',NULL,now()-interval '50 minutes'),
		('10000000-0000-4000-8000-000000000004','PROCESS_DOCUMENT','BACKGROUND',$2,'PENDING','{}',now()-interval '1 minute',NULL,now()-interval '1 minute'),
		('10000000-0000-4000-8000-000000000005','PROCESS_DOCUMENT','BACKGROUND',NULL,'SUCCEEDED','{}',now()-interval '3 hours',now()-interval '30 seconds',now()-interval '3 hours'),
		('10000000-0000-4000-8000-000000000006','PROCESS_DOCUMENT','BACKGROUND',NULL,'PENDING','{}',now()-interval '3 hours',NULL,now()-interval '3 hours')`, householdA, householdB); err != nil {
		t.Fatal(err)
	}
	// B never ran; A ran a minute ago; the platform group ran 30 seconds ago,
	// so its old job waits its turn instead of holding the head of the lane.
	want := []struct{ id, household string }{
		{"10000000-0000-4000-8000-000000000004", householdB},
		{"10000000-0000-4000-8000-000000000002", householdA},
		{"10000000-0000-4000-8000-000000000006", ""},
		{"10000000-0000-4000-8000-000000000003", householdA},
	}
	for i, expected := range want {
		job, found, err := claimIn(ctx, tx, "fairness-test", "BACKGROUND")
		if err != nil || !found {
			t.Fatalf("claim %d: found=%v err=%v", i, found, err)
		}
		if job.ID != expected.id || job.HouseholdID != expected.household {
			t.Fatalf("claim %d = %s (%s), want %s (%s)", i, job.ID, job.HouseholdID, expected.id, expected.household)
		}
	}
}

// TestJobHouseholdIsDerivedFromPayloadAnchor pins migration 00087's enqueue
// trigger: callers keep inserting payload-only jobs and still get an owner.
func TestJobHouseholdIsDerivedFromPayloadAnchor(t *testing.T) {
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
	var household, sourceEvent string
	if err := tx.QueryRow(ctx, `INSERT INTO household(name) VALUES('Job owner test') RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT','job-owner-test',now(),'\x00','RECEIVED') RETURNING id`, household).Scan(&sourceEvent); err != nil {
		t.Fatal(err)
	}
	var owner *string
	if err := tx.QueryRow(ctx, `INSERT INTO job(type,payload_json,run_after) VALUES('PROCESS_TELEGRAM_TEXT',jsonb_build_object('source_event_id',$1::uuid),now()+interval '1 day') RETURNING household_id::text`, sourceEvent).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner == nil || *owner != household {
		t.Fatalf("job household = %v, want %s", owner, household)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO job(type,payload_json,run_after) VALUES('SEND_TELEGRAM_MESSAGE','{"chat_id":"not-a-number"}',now()+interval '1 day') RETURNING household_id::text`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != nil {
		t.Fatalf("unanchored job household = %s, want NULL", *owner)
	}
	// A 19-digit chat ID overflows bigint; derivation must yield NULL, not
	// abort the enqueue.
	if err := tx.QueryRow(ctx, `INSERT INTO job(type,payload_json,run_after) VALUES('SEND_TELEGRAM_MESSAGE','{"chat_id":"9999999999999999999"}',now()+interval '1 day') RETURNING household_id::text`).Scan(&owner); err != nil {
		t.Fatalf("overflowing chat_id aborted the enqueue: %v", err)
	}
	if owner != nil {
		t.Fatalf("overflowing chat_id household = %s, want NULL", *owner)
	}
}
