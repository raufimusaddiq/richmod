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
	if got := retryDelaySeconds(job, errors.New("temporary")); got != 2 {
		t.Fatalf("backoff after attempt 1 = %ds, want 2", got)
	}
	if got := retryDelaySeconds(job, fmt.Errorf("send: %w", rateLimited{wait: 37 * time.Second})); got != 37 {
		t.Fatalf("Telegram retry_after was not honoured: %ds", got)
	}
	if got := retryDelaySeconds(job, rateLimited{wait: 3 * time.Hour}); got != 3600 {
		t.Fatalf("retry_after was not capped at one hour: %ds", got)
	}
	if got := retryDelaySeconds(Job{Attempts: 3}, rateLimited{}); got != 8 {
		t.Fatalf("a zero hint must fall back to backoff: %ds", got)
	}
}
