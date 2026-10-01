package queue

import (
	"context"
	"errors"
	"testing"
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
