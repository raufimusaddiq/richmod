package systemone

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// InstrumentedEngine records the bounded call after the wrapped engine returns.
// It logs question keys only, never state, prompts, or answer values.
type InstrumentedEngine struct {
	Engine judgment.Engine
	Record Recorder
}

func (e InstrumentedEngine) Evaluate(ctx context.Context, requestID string, input judgment.Request) (judgment.Result, error) {
	// Instrumentation wraps the engine rather than living inside it so telemetry
	// never changes the wire request: a test engine and the production client
	// both report exactly one phase per call.
	started := time.Now()
	result, err := e.Engine.Evaluate(ctx, requestID, input)
	if e.Record == nil {
		return result, err
	}
	status, class := "SUCCEEDED", ""
	if err != nil {
		status, class = "FAILED", classify(err)
	}
	phase := judgment.PhaseMetadataFromContext(ctx)
	purpose := phase.Purpose
	if purpose == "" {
		purpose = PurposeForQuestions(input.Questions)
	}
	e.Record(ctx, Metric{
		SourceEventID:      judgment.SourceEventFromContext(ctx),
		Purpose:            purpose,
		PolicyVersion:      phase.PolicyVersion,
		Dimensions:         questionKeys(input.Questions),
		AnsweredDimensions: judgment.AnsweredDimensions(input.Questions, result.Answers),
		Model:              result.Model,
		Status:             status,
		ErrorClass:         class,
		DurationMs:         time.Since(started).Milliseconds(),
		QuestionCount:      len(input.Questions),
	})
	return result, err
}

func questionKeys(questions map[string]judgment.Question) []string {
	keys := make([]string, 0, len(questions))
	for key := range questions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// PurposeForQuestions names the bounded purpose from the question shape when no
// explicit purpose was set. It never invents a fact; it only labels a phase.
func PurposeForQuestions(questions map[string]judgment.Question) string {
	if _, ok := questions["route"]; ok {
		return "ROUTE"
	}
	if _, ok := questions["category"]; ok && len(questions) == 1 {
		return "RESIDUAL_CATEGORY"
	}
	if _, ok := questions["review_action"]; ok {
		return "RESIDUAL_REVIEW_ACTION"
	}
	for key := range questions {
		if strings.Contains(key, "support") || strings.Contains(key, "observed") {
			return "EVIDENCE_SUPPORT"
		}
	}
	return "OTHER_BOUNDED"
}
