package systemone

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

type recordingEngine struct{ err error }

func (e recordingEngine) Evaluate(context.Context, string, judgment.Request) (judgment.Result, error) {
	return judgment.Result{Model: "test-model", Answers: map[string]judgment.Answer{"category": {Type: "choice"}}}, e.err
}

func TestInstrumentedEngineRecordsOneAttributedPhase(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	questions := map[string]judgment.Question{
		"category": {Type: "choice", Instructions: "pick", Criteria: map[string]any{"A": "a", "B": "b"}},
		"amount":   {Type: "noul", Instructions: "supported?"},
	}
	for _, engineErr := range []error{nil, errors.New("provider unavailable")} {
		calls := 0
		var got Metric
		engine := InstrumentedEngine{Engine: recordingEngine{err: engineErr}, Record: func(_ context.Context, metric Metric) {
			calls++
			got = metric
		}}
		ctx := judgment.WithSourceEvent(context.Background(), sourceID)
		ctx = judgment.WithPhaseMetadata(ctx, "RESIDUAL_CATEGORY", "test-policy")
		_, _ = engine.Evaluate(ctx, "request-id", judgment.Request{Questions: questions})
		if calls != 1 || got.SourceEventID != sourceID || got.Purpose != "RESIDUAL_CATEGORY" || got.PolicyVersion != "test-policy" || got.Model != "test-model" {
			t.Fatalf("calls=%d metric=%+v", calls, got)
		}
		if !reflect.DeepEqual(got.Dimensions, []string{"amount", "category"}) || !reflect.DeepEqual(got.AnsweredDimensions, []string{"category"}) {
			t.Fatalf("dimensions=%v answered=%v", got.Dimensions, got.AnsweredDimensions)
		}
		wantStatus := "SUCCEEDED"
		if engineErr != nil {
			wantStatus = "FAILED"
		}
		if got.Status != wantStatus {
			t.Fatalf("status=%s want=%s", got.Status, wantStatus)
		}
	}
}

// TestInstrumentedEnginePrefersContextSourceEvent is the attribution regression
// from the plan: document calls pass their own document ID as the request ID, so
// the trusted event the caller attached to the context must win.
func TestInstrumentedEnginePrefersContextSourceEvent(t *testing.T) {
	const eventID = "22222222-2222-4222-8222-222222222222"
	var got Metric
	engine := InstrumentedEngine{Engine: recordingEngine{}, Record: func(_ context.Context, metric Metric) { got = metric }}
	ctx := judgment.WithSourceEvent(context.Background(), eventID)
	_, _ = engine.Evaluate(ctx, "document-id-that-is-not-a-uuid", judgment.Request{Questions: map[string]judgment.Question{"category": {Type: "choice", Instructions: "pick", Criteria: map[string]any{"A": "a", "B": "b"}}}})
	if got.SourceEventID != eventID {
		t.Fatalf("sourceEventID = %q, want %q", got.SourceEventID, eventID)
	}
}
