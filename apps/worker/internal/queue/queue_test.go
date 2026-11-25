package queue

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type redactedTimeout struct{}

func (redactedTimeout) Error() string { return "generate insight: gateway_timeout" }
func (redactedTimeout) Unwrap() error { return context.DeadlineExceeded }

func TestClassifyErrorPreservesRedactedTimeoutCause(t *testing.T) {
	if got := classifyError(redactedTimeout{}); got != "TIMEOUT" {
		t.Fatalf("redacted timeout class=%s", got)
	}
}

type permanentTestError struct{}

func (permanentTestError) Error() string   { return "permanent" }
func (permanentTestError) Permanent() bool { return true }

func TestIsPermanentUnwrapsErrors(t *testing.T) {
	if isPermanent(errors.New("temporary")) {
		t.Fatal("ordinary error marked permanent")
	}
	if !isPermanent(errors.Join(errors.New("outer"), permanentTestError{})) {
		t.Fatal("wrapped permanent error was not recognized")
	}
}

type permanentError struct{}

func (permanentError) Error() string   { return "permanent" }
func (permanentError) Permanent() bool { return true }

func TestFinalIsTheLastAttemptOrAPermanentError(t *testing.T) {
	transient := errors.New("temporary")
	job := Job{Attempts: 1, MaxAttempts: 5}
	if Final(job, transient) {
		t.Fatal("a transient error on an early attempt must be retried")
	}
	if !Final(job, permanentError{}) {
		t.Fatal("a permanent error must be final even on the first attempt")
	}
	if !Final(Job{Attempts: 5, MaxAttempts: 5}, transient) {
		t.Fatal("the last allowed attempt is final")
	}
}

type rateLimited struct{ wait time.Duration }

func (e rateLimited) Error() string             { return "rate limited" }
func (e rateLimited) RetryAfter() time.Duration { return e.wait }

func TestRetryDelayHonoursRetryAfterHint(t *testing.T) {
	job := Job{Attempts: 1}
	for range 50 {
		if got := retryDelaySeconds(job, errors.New("temporary")); got < 2 || got > 3 {
			t.Fatalf("backoff after attempt 1 = %ds, want 2s plus under 20%% jitter", got)
		}
	}
	if got := retryDelaySeconds(job, fmt.Errorf("send: %w", rateLimited{wait: 37 * time.Second})); got != 37 {
		t.Fatalf("Telegram retry_after was not honoured: %ds", got)
	}
	if got := retryDelaySeconds(job, rateLimited{wait: 3 * time.Hour}); got != 3600 {
		t.Fatalf("retry_after was not capped at one hour: %ds", got)
	}
	if got := retryDelaySeconds(Job{Attempts: 3}, rateLimited{}); got < 8 || got > 10 {
		t.Fatalf("a zero hint must fall back to jittered backoff: %ds", got)
	}
}

func TestRetryDelayJitterNeverFiresBeforeTheBackoff(t *testing.T) {
	distinct := map[time.Duration]bool{}
	for range 200 {
		got := jitter(10 * time.Minute)
		if got < 10*time.Minute || got >= 12*time.Minute {
			t.Fatalf("jitter(10m) = %s, want [10m, 12m)", got)
		}
		distinct[got] = true
	}
	if len(distinct) < 2 {
		t.Fatal("jitter must spread retries")
	}
	if jitter(0) != 0 {
		t.Fatal("zero delay must stay zero")
	}
}

func TestDeferralDelayHonoursProviderBackoffUpToADay(t *testing.T) {
	if got := deferralDelayFor(0, rateLimited{wait: 3 * time.Hour}); got != 3*time.Hour {
		t.Fatalf("Retry-After above the exponential base must win exactly: %s", got)
	}
	if got := deferralDelayFor(0, fmt.Errorf("extract: %w", rateLimited{wait: 72 * time.Hour})); got != 24*time.Hour {
		t.Fatalf("provider hint must cap at 24h: %s", got)
	}
	if got := deferralDelayFor(10, rateLimited{wait: time.Second}); got != time.Hour {
		t.Fatalf("a tiny hint must not undercut the exponential base: %s", got)
	}
	for range 50 {
		if got := deferralDelayFor(1, errors.New("down")); got < 10*time.Minute || got >= 12*time.Minute {
			t.Fatalf("unhinted deferral = %s, want 10m plus under 20%% jitter", got)
		}
	}
}

func TestPermanentWrapsWithoutChangingTheCause(t *testing.T) {
	cause := errors.New("HTTP 400")
	wrapped := fmt.Errorf("extract: %w", Permanent(cause))
	if !isPermanent(wrapped) || !errors.Is(wrapped, cause) {
		t.Fatal("Permanent must be final yet still unwrap to its cause")
	}
	if !Final(Job{Attempts: 1, MaxAttempts: 5}, Permanent(cause)) {
		t.Fatal("a permanent failure is final on the first attempt")
	}
	if Permanent(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	if again := Permanent(Permanent(cause)); !isPermanent(again) {
		t.Fatal("already permanent errors stay permanent")
	}
}

func TestFinalizeContextOutlivesACancelledWorker(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, release := finalizeContext(parent)
	defer release()
	if ctx.Err() != nil {
		t.Fatalf("finalising a committed job must survive SIGTERM: %v", ctx.Err())
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > finalizeTimeout {
		t.Fatalf("finalisation must stay bounded, deadline=%v", deadline)
	}
}

func TestDeterministicJobsNeverShareALaneWithModelCalls(t *testing.T) {
	deterministic := []string{"GENERATE_CYCLE_RESIDUAL_REVIEW", "FETCH_TELEGRAM_IMAGE", "PROJECT_TELEGRAM_REVIEW"}
	for _, jobType := range deterministic {
		if got := classifyLane(jobType); got != "DETERMINISTIC" {
			t.Errorf("%s lane = %s", jobType, got)
		}
	}
	model := []string{"GENERATE_INSIGHT", "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL", "PROCESS_FINANCIAL_EMAIL_PREVIEW", "PROCESS_TELEGRAM_TEXT"}
	for _, jobType := range model {
		if lane := classifyLane(jobType); lane == "DETERMINISTIC" || lane == "INTERACTIVE" {
			t.Errorf("%s must not share a lane with deterministic work: %s", jobType, lane)
		}
	}
	for _, jobType := range []string{"PROCESS_TELEGRAM_CALLBACK", "PROCESS_TELEGRAM_CARD_ANSWER", "SEND_TELEGRAM_MESSAGE", "COMPLETE_BANK_REVIEW"} {
		if got := classifyLane(jobType); got != "INTERACTIVE" {
			t.Errorf("%s lane = %s", jobType, got)
		}
	}
}
