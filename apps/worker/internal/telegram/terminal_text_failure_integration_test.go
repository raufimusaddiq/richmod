package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
)

type terminalFixture struct {
	t           *testing.T
	pool        *pgxpool.Pool
	ctx         context.Context
	householdID string
	chatID      int64
	stamp       int64
}

func newTerminalFixture(t *testing.T) *terminalFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	stamp := time.Now().UnixNano()
	f := &terminalFixture{t: t, pool: pool, ctx: ctx, stamp: stamp, chatID: stamp%1_000_000_000 + 7_000_000_000}
	if err := pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("Terminal text %d", stamp)).Scan(&f.householdID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *terminalFixture) event(id int64) string {
	return seedTelegramRaw(f.t, f.pool, f.householdID, "TELEGRAM_TEXT", map[string]any{
		"update_id": f.stamp + id,
		"message": map[string]any{
			"message_id": id, "text": "jelaskan analisis bulan ini",
			"from": map[string]any{"id": f.chatID}, "chat": map[string]any{"id": f.chatID},
		},
	})
}

func (f *terminalFixture) job(sourceEventID string, attempts int) queue.Job {
	payload, _ := json.Marshal(map[string]string{"source_event_id": sourceEventID})
	job := queue.Job{Type: "PROCESS_TELEGRAM_TEXT", Payload: payload, Attempts: attempts, MaxAttempts: 5}
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO job(type,payload_json,status,attempts,max_attempts,locked_at,locked_by) VALUES('PROCESS_TELEGRAM_TEXT',$1::jsonb,'RUNNING',$2,5,now(),'test') RETURNING id::text`, string(payload), attempts).Scan(&job.ID); err != nil {
		f.t.Fatal(err)
	}
	return job
}

func (f *terminalFixture) eventStatus(id string) string {
	var status string
	if err := f.pool.QueryRow(f.ctx, `SELECT processing_status FROM source_event WHERE id=$1`, id).Scan(&status); err != nil {
		f.t.Fatal(err)
	}
	return status
}

func (f *terminalFixture) jobStatus(id string) string {
	var status string
	if err := f.pool.QueryRow(f.ctx, `SELECT status FROM job WHERE id=$1`, id).Scan(&status); err != nil {
		f.t.Fatal(err)
	}
	return status
}

func (f *terminalFixture) replies() int {
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'text'=$2`,
		fmt.Sprint(f.chatID), terminalTextFailureMessage).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

func timeoutError() error {
	return TerminalError(fmt.Errorf("%w: %w", errModelPhase, context.DeadlineExceeded))
}

// A typed message that will not be retried again must not end in silence, and
// the notice must be atomic with marking the job FAILED: the job is never
// FAILED without the notice having been queued.
func TestTerminalNoticeIsAtomicWithMarkingTheJobFailed(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)
	hookFor := func(eventID string) func(context.Context, pgx.Tx) error {
		return func(ctx context.Context, tx pgx.Tx) error {
			return processor.TerminalTextFailureTx(ctx, tx, eventID, true)
		}
	}

	// Final attempt: the job fails, the event fails, and one notice is queued.
	event := f.event(1)
	job := f.job(event, 2)
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hookFor(event)); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(job.ID) != "FAILED" || f.eventStatus(event) != "FAILED" || f.replies() != 1 {
		t.Fatalf("final failure: job=%s event=%s replies=%d", f.jobStatus(job.ID), f.eventStatus(event), f.replies())
	}

	// A replayed failure must not send the notice twice.
	if err := processor.HandleTerminalTextFailure(f.ctx, event, true); err != nil {
		t.Fatal(err)
	}
	if f.replies() != 1 {
		t.Fatalf("the notice must be sent once, got %d", f.replies())
	}

	// A transient failure on an early attempt retries: nothing is announced.
	early := f.event(2)
	earlyJob := f.job(early, 1)
	if err := jobs.FailWithHook(f.ctx, earlyJob, errors.New("temporary"), hookFor(early)); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(earlyJob.ID) != "PENDING" || f.eventStatus(early) != "RECEIVED" || f.replies() != 1 {
		t.Fatalf("early failure: job=%s event=%s replies=%d", f.jobStatus(earlyJob.ID), f.eventStatus(early), f.replies())
	}

	// A message that was already answered is never contradicted.
	answered := f.event(3)
	if _, err := f.pool.Exec(f.ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, answered); err != nil {
		t.Fatal(err)
	}
	answeredJob := f.job(answered, 2)
	if err := jobs.FailWithHook(f.ctx, answeredJob, timeoutError(), hookFor(answered)); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(answeredJob.ID) != "FAILED" || f.eventStatus(answered) != "PROCESSED" || f.replies() != 1 {
		t.Fatalf("answered event: job=%s event=%s replies=%d", f.jobStatus(answeredJob.ID), f.eventStatus(answered), f.replies())
	}
}

// A hook that cannot succeed must not wedge the job: it is still marked FAILED,
// and the failed notice leaves no partial state behind.
func TestFailingTerminalHookStillFailsTheJobWithoutPartialState(t *testing.T) {
	f := newTerminalFixture(t)
	jobs := queue.New(f.pool)
	event := f.event(1)
	job := f.job(event, 2)
	hook := func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1`, event); err != nil {
			return err
		}
		return errors.New("notice could not be queued")
	}
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hook); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(job.ID) != "FAILED" {
		t.Fatalf("a job whose notice failed must still be marked FAILED, got %s", f.jobStatus(job.ID))
	}
	if got := f.eventStatus(event); got != "RECEIVED" {
		t.Fatalf("the rolled-back hook must leave the event untouched, got %s", got)
	}
	if f.replies() != 0 {
		t.Fatalf("no notice may exist when the hook failed, got %d", f.replies())
	}
}

// A job that another worker already took over must not announce a failure.
func TestTerminalHookDoesNotRunForAJobThisWorkerNoLongerOwns(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)
	event := f.event(1)
	job := f.job(event, 2)
	if _, err := f.pool.Exec(f.ctx, `UPDATE job SET status='SUCCEEDED' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	hook := func(ctx context.Context, tx pgx.Tx) error {
		return processor.TerminalTextFailureTx(ctx, tx, event, true)
	}
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hook); err != nil {
		t.Fatal(err)
	}
	if f.eventStatus(event) != "RECEIVED" || f.replies() != 0 {
		t.Fatalf("a worker that lost the job must not announce a failure: event=%s replies=%d", f.eventStatus(event), f.replies())
	}
}

// A malformed or empty source event ID is a no-op, never an SQL error that would
// abort the transaction marking the job FAILED.
func TestTerminalNoticeIgnoresMalformedEventIDs(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	for _, id := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if err := processor.HandleTerminalTextFailure(f.ctx, id, true); err != nil {
			t.Fatalf("id %q must be a no-op, got %v", id, err)
		}
	}
}

// The notice and the recorded reason follow the cause: a timeout may say the
// assistant was slow, any other failure must not.
func TestTerminalNoticeNamesTheCause(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	for _, tc := range []struct {
		message  string
		timedOut bool
		reason   string
		text     string
	}{{"1", true, "TIMEOUT", terminalTextFailureMessage}, {"2", false, "ERROR", terminalTextErrorMessage}} {
		event := f.event(map[string]int64{"1": 101, "2": 102}[tc.message])
		if err := processor.HandleTerminalTextFailure(f.ctx, event, tc.timedOut); err != nil {
			t.Fatal(err)
		}
		var replies int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'text'=$2`, fmt.Sprint(f.chatID), tc.text).Scan(&replies); err != nil || replies != 1 {
			t.Fatalf("timedOut=%v: expected one reply with the matching text, got %d (%v)", tc.timedOut, replies, err)
		}
		var reason string
		if err := f.pool.QueryRow(f.ctx, `SELECT metadata_json->>'reason' FROM integration_action WHERE household_id=$1 AND dedupe_key=$2`, f.householdID, event).Scan(&reason); err != nil || reason != tc.reason {
			t.Fatalf("timedOut=%v: expected the recorded reason %s, got %q (%v)", tc.timedOut, tc.reason, reason, err)
		}
	}
}

// HandleTerminalTextFailure runs TerminalTextFailureTx in its own transaction,
// as the worker does when it fails a job.
func (p *Processor) HandleTerminalTextFailure(ctx context.Context, sourceEventID string, timedOut bool) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.TerminalTextFailureTx(ctx, tx, sourceEventID, timedOut); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
