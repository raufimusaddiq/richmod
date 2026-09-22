package telegram

import (
	"context"
	"testing"
)

// turnTrace records each bounded task once, in first-seen order, and keeps the
// model that answered it. Duplicate tasks must not inflate the count, because
// that count is reported as native tool calls avoided (PRD §23).
func TestTurnTraceRecordsEachTaskOnce(t *testing.T) {
	var trace turnTrace
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
	trace.reset()
	if trace.consumed() || trace.model != "" {
		t.Fatalf("reset must clear the trace, got %+v", trace)
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

	p.turnTrace.reset()
	p.turnTrace.record(judgmentTaskRoute, "jev-1")
	p.turnTrace.record(judgmentTaskReviewAction, "jev-1")
	p.recordTurnTelemetry(ctx, f.householdID, f.sourceID, judgmentTurnObservation{
		Lane:                   judgmentLaneJevOnly,
		DecisionTasks:          p.turnTrace.tasks,
		Model:                  p.turnTrace.model,
		NativeToolCallsAvoided: len(p.turnTrace.tasks),
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
