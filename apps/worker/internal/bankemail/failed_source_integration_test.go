package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type failedBankFixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	ctx       context.Context
	household string
	listener  string
	stamp     int64
}

func newFailedBankFixture(t *testing.T) *failedBankFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &failedBankFixture{t: t, pool: pool, ctx: ctx, stamp: time.Now().UnixNano()}
	var user string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Failed source %d", f.stamp)).Scan(&f.household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("failed-source-%d@test.invalid", f.stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Synthetic Bank',$2,$3) RETURNING id`, f.household, fmt.Sprintf("failed-source-%d@test.invalid", f.stamp), user).Scan(&f.listener); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *failedBankFixture) source(n int) string {
	var id string
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, f.household, fmt.Sprintf("failed-source-%d-%d", f.stamp, n), []byte(fmt.Sprintf("failed-%d", n))).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *failedBankFixture) status(id string) string {
	var status string
	if err := f.pool.QueryRow(f.ctx, `SELECT processing_status FROM source_event WHERE id=$1`, id).Scan(&status); err != nil {
		f.t.Fatal(err)
	}
	return status
}

func (f *failedBankFixture) actions(id string) (count int, reason string) {
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*),COALESCE(max(metadata_json->>'reason'),'') FROM integration_action WHERE household_id=$1 AND integration_type='SOURCE_PROCESSING' AND action_type='SOURCE_FAILED' AND status='OPEN' AND dedupe_key=$2`, f.household, id).Scan(&count, &reason); err != nil {
		f.t.Fatal(err)
	}
	return count, reason
}

func (f *failedBankFixture) inTx(fn func(pgx.Tx) error) {
	f.t.Helper()
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err := fn(tx); err != nil {
		f.t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
}

// An unusable extraction is final (the job succeeds), so the household is told
// at once with a dismissable item, and still gets no review (ADR-048).
func TestInvalidExtractionLeavesADismissableItemAndNoReview(t *testing.T) {
	f := newFailedBankFixture(t)
	source := f.source(1)
	processor := &Processor{pool: f.pool}
	for i := 0; i < 2; i++ { // a replay must not duplicate the item
		if err := processor.persistExtractionFailure(f.ctx, source, f.listener, "stub", "INVALID", "REPAIR"); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.status(source); got != "FAILED" {
		t.Fatalf("an unusable extraction is FAILED, got %s", got)
	}
	if count, reason := f.actions(source); count != 1 || reason != "INVALID" {
		t.Fatalf("expected one INVALID item, got %d (%s)", count, reason)
	}
	var reviews int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM review_item WHERE source_event_id=$1`, source).Scan(&reviews); err != nil || reviews != 0 {
		t.Fatalf("an unusable extraction must not open a review: %d (%v)", reviews, err)
	}
}

// A retryable failure is not announced while attempts remain: a retry that
// succeeds must not leave a stale item behind. It is announced when the job
// gives up.
func TestRetryableFailureIsAnnouncedOnlyWhenTheJobGivesUp(t *testing.T) {
	f := newFailedBankFixture(t)
	source := f.source(2)
	processor := &Processor{pool: f.pool}
	if err := processor.persistExtractionFailure(f.ctx, source, f.listener, "stub", "TRANSPORT_FAILED", "RETRY"); err != nil {
		t.Fatal(err)
	}
	if count, _ := f.actions(source); count != 0 {
		t.Fatalf("a retryable failure must not be announced while attempts remain, got %d", count)
	}

	f.inTx(func(tx pgx.Tx) error { return processor.TerminalFailureTx(f.ctx, tx, source) })
	if got := f.status(source); got != "FAILED" {
		t.Fatalf("status %s", got)
	}
	if count, reason := f.actions(source); count != 1 || reason != "TRANSPORT_FAILED" {
		t.Fatalf("the final failure must leave one item naming the cause, got %d (%s)", count, reason)
	}
	f.inTx(func(tx pgx.Tx) error { return processor.TerminalFailureTx(f.ctx, tx, source) })
	if count, _ := f.actions(source); count != 1 {
		t.Fatalf("a replay must not duplicate the item, got %d", count)
	}

	// An email that finished meanwhile is never reopened or announced.
	done := f.source(3)
	if _, err := f.pool.Exec(f.ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, done); err != nil {
		t.Fatal(err)
	}
	f.inTx(func(tx pgx.Tx) error { return processor.TerminalFailureTx(f.ctx, tx, done) })
	if got := f.status(done); got != "PROCESSED" {
		t.Fatalf("a processed email must stay PROCESSED, got %s", got)
	}
	if count, _ := f.actions(done); count != 0 {
		t.Fatalf("a processed email must not be announced, got %d", count)
	}

	// A malformed ID is a no-op, never an SQL error that aborts the transaction.
	f.inTx(func(tx pgx.Tx) error { return processor.TerminalFailureTx(f.ctx, tx, "not-a-uuid") })
}
