package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
)

type permanentFailure interface{ Permanent() bool }

func isPermanentFailure(err error) bool {
	var permanent permanentFailure
	return errors.As(err, &permanent) && permanent.Permanent()
}

func TestSettleDefersEveryModelJobWhileTheGatewayIsDown(t *testing.T) {
	down := fmt.Errorf("extract: %w", &gateway.StatusError{StatusCode: 503, Endpoint: "responses"})
	unreachable := &gateway.UnavailableError{Err: errors.New("connection refused")}
	for _, jobType := range []string{"PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL", "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "GENERATE_INSIGHT"} {
		for _, cause := range []error{down, unreachable} {
			action, effective := settle(false, queue.Job{Type: jobType, Attempts: 3, MaxAttempts: 3}, cause)
			if action != settleDefer || !errors.Is(effective, cause) {
				t.Errorf("%s with %v: action=%v, want defer so the attempt is not burned", jobType, cause, action)
			}
		}
	}
}

func TestSettleFailsPreviewsFastBecauseTheUIGivesUpAfterThirtySeconds(t *testing.T) {
	action, _ := settle(false, queue.Job{Type: "PROCESS_FINANCIAL_EMAIL_PREVIEW"}, &gateway.StatusError{StatusCode: 503})
	if action != settleFail {
		t.Fatalf("an interactive preview must not wait hours, action=%v", action)
	}
}

func TestSettleStopsDeferringAfterTheDeferralBudget(t *testing.T) {
	down := &gateway.StatusError{StatusCode: 503}
	if action, _ := settle(false, queue.Job{Type: "PROCESS_BANK_EMAIL", Deferrals: maxModelJobDeferrals - 1}, down); action != settleDefer {
		t.Fatalf("below budget action=%v", action)
	}
	if action, _ := settle(false, queue.Job{Type: "PROCESS_BANK_EMAIL", Deferrals: maxModelJobDeferrals}, down); action != settleFail {
		t.Fatalf("an outage longer than the budget must fail into review, action=%v", action)
	}
}

func TestSettleFailsFastOnPermanentGatewayRejection(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422} {
		cause := &gateway.StatusError{StatusCode: status}
		for _, jobType := range []string{"PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL_PREVIEW", "PROCESS_DOCUMENT", "GENERATE_INSIGHT"} {
			action, effective := settle(false, queue.Job{Type: jobType, Attempts: 1, MaxAttempts: 5}, cause)
			if action != settleFail || !isPermanentFailure(effective) || !queue.Final(queue.Job{Attempts: 1, MaxAttempts: 5}, effective) {
				t.Errorf("HTTP %d on %s must be a permanent failure on the first attempt", status, jobType)
			}
		}
	}
	timeout := &gateway.StatusError{StatusCode: 408}
	if _, effective := settle(false, queue.Job{Type: "PROCESS_RECEIPT", Attempts: 1, MaxAttempts: 5}, timeout); isPermanentFailure(effective) {
		t.Fatal("a 408 is retryable")
	}
}

func TestSettleLeavesDeterministicAndOrdinaryFailuresOnTheAttemptBudget(t *testing.T) {
	ordinary := errors.New("database is locked")
	for _, jobType := range []string{"PROCESS_BANK_EMAIL", "GENERATE_CYCLE_RESIDUAL_REVIEW", "SEND_TELEGRAM_MESSAGE", "PROJECT_TELEGRAM_REVIEW"} {
		action, effective := settle(false, queue.Job{Type: jobType, Attempts: 1, MaxAttempts: 5}, ordinary)
		if action != settleFail || effective != ordinary {
			t.Errorf("%s: action=%v effective=%v", jobType, action, effective)
		}
	}
	down := &gateway.StatusError{StatusCode: 503}
	if action, _ := settle(false, queue.Job{Type: "GENERATE_CYCLE_RESIDUAL_REVIEW"}, down); action != settleFail {
		t.Fatalf("a deterministic job never waits on the gateway, action=%v", action)
	}
}

func TestSettleReleasesAJobCancelledByShutdownWithoutBurningAnAttempt(t *testing.T) {
	cancelled := fmt.Errorf("process: %w", context.Canceled)
	if action, _ := settle(true, queue.Job{Type: "PROCESS_DOCUMENT"}, cancelled); action != settleRelease {
		t.Fatalf("shutdown cancellation action=%v", action)
	}
	if action, _ := settle(false, queue.Job{Type: "PROCESS_DOCUMENT"}, cancelled); action == settleRelease {
		t.Fatal("a cancellation while the worker keeps running is an ordinary failure")
	}
	if action, _ := settle(true, queue.Job{Type: "PROCESS_DOCUMENT"}, context.DeadlineExceeded); action == settleRelease {
		t.Fatal("a job budget timeout is a real failure even during shutdown")
	}
}

func TestWaitForLanesReportsAStuckLoop(t *testing.T) {
	var finished sync.WaitGroup
	finished.Go(func() {})
	if !waitForLanes(&finished, time.Second) {
		t.Fatal("finished loops must drain")
	}
	release := make(chan struct{})
	var stuck sync.WaitGroup
	stuck.Go(func() { <-release })
	defer close(release)
	if waitForLanes(&stuck, 20*time.Millisecond) {
		t.Fatal("a loop that never stops must hit the shutdown deadline")
	}
}

func TestPhaseMetricNeverCarriesANonUUIDSourceEvent(t *testing.T) {
	for _, bogus := range []string{"preview:6f1d3c0e-0000-4000-8000-000000000001", "not-a-uuid"} {
		if got := phaseMetric(gateway.CallMetric{SourceEventID: bogus}, "").SourceEventID; got != "" {
			t.Errorf("%q must not reach the source_event foreign key, got %q", bogus, got)
		}
	}
	const valid = "6f1d3c0e-0000-4000-8000-000000000001"
	if got := phaseMetric(gateway.CallMetric{}, valid).SourceEventID; got != valid {
		t.Fatalf("fallback source event lost: %q", got)
	}
}
