package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// failingEngine answers nothing: every bounded call is a provider failure.
type failingEngine struct{}

func (failingEngine) Evaluate(context.Context, string, judgment.Request) (judgment.Result, error) {
	return judgment.Result{}, errors.New("provider down")
}

// A provider failure is not a consumed Jev decision. It must not be recorded in
// the turn trace, or the turn would report JEV_ONLY and count avoided generative
// calls it never actually avoided (Hermes PR #103).
func TestTurnTraceIgnoresFailedEvaluations(t *testing.T) {
	ctx, trace := withTurnTrace(context.Background())
	p := NewProcessor(nil, nil)
	p.SetJudgment(failingEngine{})
	if _, err := p.evaluate(ctx, judgmentTaskRoute, "req", judgment.Request{}); err == nil {
		t.Fatal("expected the failing engine to surface an error")
	}
	if trace.consumed() {
		t.Fatalf("a provider failure must not count as a consumed decision, got %v", trace.tasks)
	}
}

// turnTrace records each bounded task once, in first-seen order, and keeps the
// model that answered it. Duplicate tasks must not inflate the count, because
// that count is reported as native tool calls avoided (PRD §23).
func TestTurnTraceRecordsEachTaskOnce(t *testing.T) {
	trace := &turnTrace{}
	if trace.consumed() {
		t.Fatal("a fresh trace must not report consumed decisions")
	}
	trace.record(judgmentTaskRoute, "jev-1")
	trace.record(judgmentTaskTransferPurpose, "jev-1")
	trace.record(judgmentTaskRoute, "jev-1")
	if got := len(trace.tasks); got != 2 {
		t.Fatalf("expected 2 distinct tasks, got %d (%v)", got, trace.tasks)
	}
	if trace.model != "jev-1" {
		t.Fatalf("model=%q, want jev-1", trace.model)
	}
}

// The trace must travel with the turn, not with the Processor: two concurrent
// turns on one Processor must not see each other's bounded decisions.
func TestTurnTraceIsScopedToTheTurnContext(t *testing.T) {
	firstCtx, firstTrace := withTurnTrace(context.Background())
	secondCtx, secondTrace := withTurnTrace(context.Background())
	if firstTrace == secondTrace {
		t.Fatal("each turn must get its own trace")
	}
	firstTrace.record(judgmentTaskRoute, "jev-1")
	if turnTraceFrom(firstCtx) != firstTrace || turnTraceFrom(secondCtx) != secondTrace {
		t.Fatal("traces must be retrievable from their own turn context")
	}
	if turnTraceFrom(secondCtx).consumed() {
		t.Fatal("a concurrent turn must not observe another turn's decisions")
	}
	if turnTraceFrom(context.Background()) != nil {
		t.Fatal("a context without a turn trace must report none")
	}
}

// The turn lane is the whole point of the telemetry: a turn the judgment plane
// resolved alone is JEV_ONLY and reports its bounded tasks as avoided
// generative calls. A turn that then ran the generative loop is mixed.
func TestTurnTelemetryRecordsLaneAndAvoidedCalls(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-telemetry")
	p := NewProcessor(f.pool, nil)
	p.SetTurnTelemetry(true)
	t.Cleanup(f.pool.Close)
	defer p.pool.Close()

	trace := &turnTrace{}
	trace.record(judgmentTaskRoute, "jev-1")
	trace.record(judgmentTaskReviewAction, "jev-1")
	p.recordTurnTelemetry(ctx, f.householdID, f.sourceID, judgmentTurnObservation{
		Lane:                   judgmentLaneJevOnly,
		DecisionTasks:          trace.tasks,
		Model:                  trace.model,
		NativeToolCallsAvoided: len(trace.tasks),
	})

	var lane, policyVersion string
	var tasks []string
	var avoided int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT lane,decision_tasks,policy_version,native_tool_calls_avoided FROM judgment_turn_telemetry WHERE household_id=$1 AND source_event_id=$2`, f.householdID, f.sourceID).Scan(&lane, &tasks, &policyVersion, &avoided))
	if lane != string(judgmentLaneJevOnly) {
		t.Fatalf("lane=%s, want JEV_ONLY", lane)
	}
	if avoided != 2 {
		t.Fatalf("native_tool_calls_avoided=%d, want 2", avoided)
	}
	if policyVersion != judgmentPolicyVersion {
		t.Fatalf("policy_version=%s, want %s", policyVersion, judgmentPolicyVersion)
	}
	if len(tasks) != 2 {
		t.Fatalf("decision_tasks=%v, want 2 entries", tasks)
	}
}

// Disabled telemetry must write nothing, so tests and unconfigured environments
// keep the previous behaviour.
func TestTurnTelemetryDisabledWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-telemetry-off")
	p := NewProcessor(f.pool, nil)
	p.recordTurnTelemetry(ctx, f.householdID, f.sourceID, judgmentTurnObservation{Lane: judgmentLaneGenerativeOnly})
	var count int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_turn_telemetry WHERE household_id=$1`, f.householdID).Scan(&count))
	if count != 0 {
		t.Fatalf("disabled telemetry wrote %d row(s)", count)
	}
}
