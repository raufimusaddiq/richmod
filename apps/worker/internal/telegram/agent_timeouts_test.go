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

// A turn is bounded by its phases, not by a wall clock tuned to one question:
// every model call gets the same cap, and the turn backstop must cover all of
// the phases at that cap, so a multi-read turn is never cut short. The household
// is told the answer is coming after ProgressNoticeDelay.
func TestAgentTimeoutsCoverTheWholePhaseChain(t *testing.T) {
	limits := defaultAgentLimits
	// Long-form analytics output measured 9 to 11 s and a first call 6 s; the old
	// 8 s cap failed both, so the cap needs real headroom over the slowest seen.
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
	// Past 15 s of silence the household starts to wonder.
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
		// Message text alone is not enough, so rewording a wrap cannot silently
		// change the retry rule in either direction.
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

// The error the loop really returns must satisfy the check the worker uses, so
// a refactor of the wrap cannot quietly bring back five identical retries.
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

// A ref the model copied from an earlier answer is not a failure of the
// household's question: the model is told to fetch fresh refs and the turn goes
// on. Any other read error stays an error.
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

// The apology blames slowness only when the model was slow.
func TestTerminalTextCopyDependsOnTheCause(t *testing.T) {
	slow, slowReason := terminalTextFailureCopy(true)
	other, otherReason := terminalTextFailureCopy(false)
	if slowReason != "TIMEOUT" || otherReason != "ERROR" {
		t.Fatalf("reasons: %s / %s", slowReason, otherReason)
	}
	if !strings.Contains(slow, "lambat") {
		t.Fatalf("a timeout may say the assistant was slow: %q", slow)
	}
	if strings.Contains(other, "lambat") || !strings.Contains(other, "tulis ulang") {
		t.Fatalf("a non-timeout failure must not blame slowness and should say how to retry: %q", other)
	}
}
