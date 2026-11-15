package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAgentTimeoutsCoverTheWholePhaseChain(t *testing.T) {
	limits := defaultAgentLimits
	// Long-form analytics output measured 9 to 11 s and a first call 6 s; the cap needs real headroom over
	// the slowest seen.
	if limits.ModelCallTimeout < 20*time.Second {
		t.Fatalf("model call cap %s leaves no headroom over a 10.7 s measured call", limits.ModelCallTimeout)
	}
	chain := time.Duration(limits.MaxModelPhases) * limits.ModelCallTimeout
	if limits.TotalTurnTimeout < chain {
		t.Fatalf("turn timeout %s is shorter than the full phase chain %s, so a multi-read turn could be cut off", limits.TotalTurnTimeout, chain)
	}
	if TextJobBudget <= limits.TotalTurnTimeout {
		t.Fatalf("job budget %s must exceed the turn timeout %s so reads, writes, and the reply fit", TextJobBudget, limits.TotalTurnTimeout)
	}
	if TextJobBudget >= 5*time.Minute {
		t.Fatalf("job budget %s must stay under the five-minute job lease", TextJobBudget)
	}
	if MaxModelTimeoutAttempts != 2 {
		t.Fatalf("a model timeout is retried once, got %d attempts", MaxModelTimeoutAttempts)
	}
}

func TestProgressNoticeWaitsLongEnoughToStaySilentForFastTurnsButNotMuchLonger(t *testing.T) {
	// Single-phase turns measured p90 about 6 s; a notice before that is noise.
	if ProgressNoticeDelay < 8*time.Second {
		t.Fatalf("notice delay %s would fire on ordinary turns", ProgressNoticeDelay)
	}
	if ProgressNoticeDelay > 15*time.Second {
		t.Fatalf("notice delay %s leaves the household waiting in silence", ProgressNoticeDelay)
	}
	if ProgressNoticeDelay >= defaultAgentLimits.ModelCallTimeout {
		t.Fatal("the notice must arrive before a single slow call can time out")
	}
}

func TestIsModelTimeoutKeysOnTheTypedSentinelNotMessageText(t *testing.T) {
	wrapped := fmt.Errorf("%w: %w", errModelPhase, fmt.Errorf("call chat completion gateway: %w", context.DeadlineExceeded))
	stringyDeadline := fmt.Errorf("%w: %w", errModelPhase, errors.New(`Post "http://router/v1/chat/completions": context deadline exceeded`))
	for _, err := range []error{wrapped, stringyDeadline} {
		if !IsModelTimeout(err) {
			t.Errorf("%v must be a model timeout", err)
		}
	}
	for _, err := range []error{
		nil,
		fmt.Errorf("%w: %w", errModelPhase, errors.New("connection refused")),
		fmt.Errorf("conversational read batch: %w", context.DeadlineExceeded),
		context.DeadlineExceeded,
		// Message text alone is not enough: rewording a wrap must not change the retry rule.
		errors.New("conversational model phase: context deadline exceeded"),
	} {
		if IsModelTimeout(err) {
			t.Errorf("%v must not count as a model timeout", err)
		}
	}
}

type timingOutModel struct{}

func (timingOutModel) AgentTurn(context.Context, string, gateway.AgentRequest) (gateway.AgentResponse, error) {
	return gateway.AgentResponse{}, fmt.Errorf("call chat completion gateway: %w", context.DeadlineExceeded)
}

// The error the loop returns must satisfy the worker's check, so a wrap refactor cannot bring back five
// identical retries.
func TestAgentLoopReturnsARecognizableModelTimeout(t *testing.T) {
	err := (&Processor{}).runAgentLoop(context.Background(), timingOutModel{}, &agentState{})
	if err == nil {
		t.Fatal("expected the model failure to be returned")
	}
	if !IsModelTimeout(err) {
		t.Fatalf("the loop's model timeout is not recognized: %v", err)
	}
	if got := err.Error(); len(got) < 27 || got[:27] != "conversational model phase:" {
		t.Fatalf("the message must keep its prefix for logs, got %q", got)
	}
}

func TestTerminalErrorIsPermanentAndKeepsItsCause(t *testing.T) {
	cause := fmt.Errorf("%w: %w", errModelPhase, context.DeadlineExceeded)
	wrapped := TerminalError(cause)
	var permanent interface{ Permanent() bool }
	if !errors.As(wrapped, &permanent) || !permanent.Permanent() {
		t.Fatal("a terminal error must be permanent so the queue stops retrying")
	}
	if !errors.Is(wrapped, context.DeadlineExceeded) || !IsModelTimeout(wrapped) {
		t.Fatal("wrapping must keep the cause visible to errors.Is and the timeout check")
	}
	if wrapped.Error() != cause.Error() {
		t.Fatalf("the message must be unchanged, got %q", wrapped.Error())
	}
	if TerminalError(nil) != nil {
		t.Fatal("wrapping nil must stay nil")
	}
}

func TestAnExpiredCategoryRefBecomesAToolResultTheModelCanActOn(t *testing.T) {
	base := agentToolResult{CallID: "c1", Tool: "get_category_drivers", Class: agentToolRead, Status: "OK"}
	for _, err := range []error{analyticscore.ErrCategoryRefNotIssued, fmt.Errorf("read failed: %w", analyticscore.ErrCategoryRefNotIssued)} {
		result, ok := recoverableAnalyticsRead(base, err)
		if !ok {
			t.Fatalf("%v must be recoverable", err)
		}
		if result.Status != "REFERENCE_NOT_ISSUED" || result.CallID != "c1" || result.Tool != "get_category_drivers" {
			t.Fatalf("unexpected result %+v", result)
		}
		next, _ := result.Facts["next_step"].(string)
		if !strings.Contains(next, "get_cycle_changes") {
			t.Fatalf("the result must tell the model what to do next, got %q", next)
		}
	}
	for _, err := range []error{nil, errors.New("database unavailable"), context.DeadlineExceeded, errors.New("category_ref required")} {
		if _, ok := recoverableAnalyticsRead(base, err); ok {
			t.Fatalf("%v must stay an error", err)
		}
	}
}

func TestTerminalTextFailurePreservesCause(t *testing.T) {
	_, slowReason := terminalTextFailureCopy(classifyTextFailure(fmt.Errorf("%w: %w", errModelPhase, context.DeadlineExceeded)))
	_, otherReason := terminalTextFailureCopy(classifyTextFailure(errors.New("boom")))
	_, downReason := terminalTextFailureCopy(classifyTextFailure(fmt.Errorf("%w: %w", errModelPhase, &gateway.StatusError{StatusCode: 503})))
	if slowReason != "TIMEOUT" || otherReason != "ERROR" || downReason != "UNAVAILABLE" {
		t.Fatalf("reasons: %s / %s / %s", slowReason, otherReason, downReason)
	}
}

func TestTextFailurePolicyBoundsRetriesByClass(t *testing.T) {
	timeout := fmt.Errorf("%w: %w", errModelPhase, context.DeadlineExceeded)
	down := fmt.Errorf("%w: %w", errModelPhase, &gateway.UnavailableError{Err: errors.New("connection refused")})
	rejected := fmt.Errorf("%w: %w", errModelPhase, &gateway.StatusError{StatusCode: 400})
	transient := errors.New("conn reset by peer")
	permanent := func(err error) bool {
		var p interface{ Permanent() bool }
		return errors.As(err, &p) && p.Permanent()
	}
	cases := []struct {
		err      error
		attempts int
		final    bool
	}{
		{timeout, 1, false}, {timeout, 2, true},
		{down, 1, false}, {down, 2, true},
		{&GatewayDeferral{Err: down}, 9, false},
		{rejected, 1, true},
		{transient, 1, false}, {transient, 4, false},
	}
	for _, c := range cases {
		if got := permanent(TextFailurePolicy(c.err, c.attempts)); got != c.final {
			t.Fatalf("%v at attempt %d: permanent=%v, want %v", c.err, c.attempts, got, c.final)
		}
	}
	if TextFailurePolicy(nil, 1) != nil {
		t.Fatal("nil stays nil")
	}
}
