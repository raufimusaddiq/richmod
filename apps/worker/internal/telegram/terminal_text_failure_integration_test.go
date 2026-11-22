package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
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
	stamp := uniqueStamp()
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
	job := queue.Job{LockedBy: "test", Type: "PROCESS_TELEGRAM_TEXT", Payload: payload, Attempts: attempts, MaxAttempts: 5}
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

func TestTerminalNoticeIsAtomicWithMarkingTheJobFailed(t *testing.T) {
	t.Parallel()
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)
	hookFor := func(eventID string) func(context.Context, pgx.Tx) error {
		return func(ctx context.Context, tx pgx.Tx) error {
			return processor.TerminalTextFailureTx(ctx, tx, eventID, testTimeoutCause)
		}
	}

	event := f.event(1)
	job := f.job(event, 2)
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hookFor(event)); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(job.ID) != "FAILED" || f.eventStatus(event) != "FAILED" || f.replies() != 1 {
		t.Fatalf("final failure: job=%s event=%s replies=%d", f.jobStatus(job.ID), f.eventStatus(event), f.replies())
	}

	if err := processor.HandleTerminalTextFailure(f.ctx, event, testTimeoutCause); err != nil {
		t.Fatal(err)
	}
	if f.replies() != 1 {
		t.Fatalf("the notice must be sent once, got %d", f.replies())
	}

	early := f.event(2)
	earlyJob := f.job(early, 1)
	if err := jobs.FailWithHook(f.ctx, earlyJob, errors.New("temporary"), hookFor(early)); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(earlyJob.ID) != "PENDING" || f.eventStatus(early) != "RECEIVED" || f.replies() != 1 {
		t.Fatalf("early failure: job=%s event=%s replies=%d", f.jobStatus(earlyJob.ID), f.eventStatus(early), f.replies())
	}

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

func TestFailingTerminalHookStillFailsTheJobWithoutPartialState(t *testing.T) {
	t.Parallel()
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

func TestTerminalHookDoesNotRunForAJobThisWorkerNoLongerOwns(t *testing.T) {
	t.Parallel()
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)
	event := f.event(1)
	job := f.job(event, 2)
	if _, err := f.pool.Exec(f.ctx, `UPDATE job SET status='SUCCEEDED' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	hook := func(ctx context.Context, tx pgx.Tx) error {
		return processor.TerminalTextFailureTx(ctx, tx, event, testTimeoutCause)
	}
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hook); err != nil {
		t.Fatal(err)
	}
	if f.eventStatus(event) != "RECEIVED" || f.replies() != 0 {
		t.Fatalf("a worker that lost the job must not announce a failure: event=%s replies=%d", f.eventStatus(event), f.replies())
	}
}

func TestTerminalNoticeIgnoresMalformedEventIDs(t *testing.T) {
	t.Parallel()
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	for _, id := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if err := processor.HandleTerminalTextFailure(f.ctx, id, testTimeoutCause); err != nil {
			t.Fatalf("id %q must be a no-op, got %v", id, err)
		}
	}
}

func TestTerminalNoticeNamesTheCause(t *testing.T) {
	t.Parallel()
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	for _, tc := range []struct {
		message string
		cause   error
		reason  string
		text    string
	}{
		{"1", testTimeoutCause, "TIMEOUT", terminalTextFailureMessage},
		{"2", errors.New("database unavailable"), "ERROR", terminalTextErrorMessage},
		{"3", fmt.Errorf("%w: %w", errModelPhase, &gateway.UnavailableError{Err: errors.New("dial tcp: connection refused")}), "UNAVAILABLE", terminalTextUnavailableMessage},
	} {
		event := f.event(map[string]int64{"1": 101, "2": 102, "3": 103}[tc.message])
		if err := processor.HandleTerminalTextFailure(f.ctx, event, tc.cause); err != nil {
			t.Fatal(err)
		}
		var replies int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'text'=$2`, fmt.Sprint(f.chatID), tc.text).Scan(&replies); err != nil || replies != 1 {
			t.Fatalf("%s: expected one reply with the matching text, got %d (%v)", tc.reason, replies, err)
		}
		if !strings.Contains(tc.text, "belum ada yang dicatat") || strings.Contains(tc.text, "kata lain") {
			t.Fatalf("%s: the notice must say nothing was recorded and not blame the wording: %q", tc.reason, tc.text)
		}
		var reason string
		if err := f.pool.QueryRow(f.ctx, `SELECT metadata_json->>'reason' FROM integration_action WHERE household_id=$1 AND dedupe_key=$2`, f.householdID, event).Scan(&reason); err != nil || reason != tc.reason {
			t.Fatalf("%s: expected the recorded reason, got %q (%v)", tc.reason, reason, err)
		}
		var failure, text string
		if err := f.pool.QueryRow(f.ctx, `SELECT public_context_json->>'failure',message_text FROM telegram_conversation_turn WHERE source_event_id=$1 AND role='ASSISTANT'`, event).Scan(&failure, &text); err != nil || failure != tc.reason || text != tc.text {
			t.Fatalf("%s: expected an ASSISTANT failure turn, got %q %q (%v)", tc.reason, failure, text, err)
		}
	}
}

var testTimeoutCause = fmt.Errorf("%w: %w", errModelPhase, context.DeadlineExceeded)

func (p *Processor) HandleTerminalTextFailure(ctx context.Context, sourceEventID string, cause error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.TerminalTextFailureTx(ctx, tx, sourceEventID, cause); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
