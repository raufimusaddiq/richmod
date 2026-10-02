package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// A turn is bounded by its phases, not by a wall clock tuned to one question:
// the turn timeout must cover one tool-selection call plus every remaining
// phase at the answer cap, so a multi-read turn is never cut short by the
// backstop. The household is told the answer is coming after ProgressNoticeDelay.
func TestAgentTimeoutsCoverTheWholePhaseChain(t *testing.T) {
	limits := defaultAgentLimits
	if limits.AnswerPhaseTimeout <= limits.PerModelCallTimeout {
		t.Fatal("the answer phase must get more time than a tool-selection call")
	}
	// Long-form analytics output on the production models measured 9 to 11 s; the
	// old 8 s cap failed it every time, so the answer cap needs real headroom.
	if limits.AnswerPhaseTimeout < 20*time.Second {
		t.Fatalf("answer phase cap %s leaves no headroom over a 10.7 s measured answer", limits.AnswerPhaseTimeout)
	}
	chain := limits.PerModelCallTimeout + time.Duration(limits.MaxModelPhases-1)*limits.AnswerPhaseTimeout
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
	if ProgressNoticeDelay >= defaultAgentLimits.AnswerPhaseTimeout {
		t.Fatal("the notice must arrive before a single slow answer call can time out")
	}
}

func TestAgentPhaseTimeoutLengthensOnlyTheAnswerCall(t *testing.T) {
	first := &agentState{}
	if got := agentPhaseTimeout(first); got != defaultAgentLimits.PerModelCallTimeout {
		t.Fatalf("the first call chooses tools and keeps the short cap, got %s", got)
	}
	answer := &agentState{PendingToolOutputs: []gateway.AgentToolOutput{{CallID: "c1", Output: map[string]any{"ok": true}}}}
	if got := agentPhaseTimeout(answer); got != defaultAgentLimits.AnswerPhaseTimeout {
		t.Fatalf("the call that carries tool results writes the answer and gets the long cap, got %s", got)
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
