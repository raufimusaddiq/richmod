package telegram

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// errJudgmentUnavailable is the single sentinel for "the bounded decision plane
// could not answer". Callers must fail closed: it is infrastructure state, not
// semantic uncertainty (PRD §9).
var errJudgmentUnavailable = errors.New("judgment plane is not configured")

// judgmentPolicyVersion identifies the threshold policy set that produced a
// decision. Reproducibility requires model version + policy version + bounded
// answers, so every persisted decision carries this (PRD §18).
const judgmentPolicyVersion = "2026-09-jev3"

// judgmentTask names one bounded decision task. Telemetry and provenance group
// by task so review/clarification rates can be computed per decision task
// (PRD §17) instead of only per protocol call.
type judgmentTask string

const (
	judgmentTaskRoute            judgmentTask = "ROUTE"
	judgmentTaskTransaction      judgmentTask = "TRANSACTION_SEMANTICS"
	judgmentTaskPendingAction    judgmentTask = "PENDING_ACTION"
	judgmentTaskPendingBatch     judgmentTask = "PENDING_BATCH"
	judgmentTaskSalaryChoice     judgmentTask = "SALARY_CHOICE"
	judgmentTaskMerchantLearning judgmentTask = "MERCHANT_LEARNING"
	judgmentTaskReviewAction     judgmentTask = "REVIEW_ACTION"
	judgmentTaskTransferPurpose  judgmentTask = "TRANSFER_PURPOSE"
)

// judgmentOutcome classifies what Go policy did with a bounded answer. The
// provider-failure outcomes are separate from the semantic ones on purpose:
// a timeout is not the same product event as an uncertain model (PRD §9).
type judgmentOutcome string

const (
	judgmentOutcomeAccepted            judgmentOutcome = "ACCEPTED"
	judgmentOutcomeClarification       judgmentOutcome = "CLARIFICATION"
	judgmentOutcomeReview              judgmentOutcome = "REVIEW"
	judgmentOutcomeRejected            judgmentOutcome = "REJECTED"
	judgmentOutcomeProviderFailure     judgmentOutcome = "PROVIDER_FAILURE"
	judgmentOutcomeJudgmentUnavailable judgmentOutcome = "JUDGMENT_UNAVAILABLE"
)

// judgmentOutcomeFor maps a policy result onto a telemetry outcome. A provider
// error is infrastructure, never semantic uncertainty.
func judgmentOutcomeFor(err error, accepted, needsClarification bool) judgmentOutcome {
	if err != nil {
		return judgmentOutcomeProviderFailure
	}
	if accepted {
		return judgmentOutcomeAccepted
	}
	if needsClarification {
		return judgmentOutcomeClarification
	}
	return judgmentOutcomeReview
}

// judgmentPolicy is the single source of truth for every threshold this worker
// applies. Values live here, in one place, with one version, instead of being
// scattered across call sites where they can drift apart (PRD §25).
var judgmentPolicy = struct {
	Version string
	Route   judgment.ChoicePolicy
	Server  judgment.ChoicePolicy
	// Transaction governs whether a transaction direction was chosen safely.
	Transaction judgment.ChoicePolicy
	Category    judgment.ChoicePolicy
	// TransferPurpose decides which canonical transfer purpose a resolved
	// source/destination pair represents. It is a bounded Choice over Go-owned
	// candidates, so it carries the same strictness as the other choices.
	TransferPurpose judgment.ChoicePolicy
	// Support Nouls answer "does this harvested candidate belong to the request?".
	AmountSupport judgment.NoulPolicy
	DateSupport   judgment.NoulPolicy
	// Ambiguity answers "is this request materially ambiguous?". Its High
	// threshold is also the ceiling above which generative self-reported
	// confidence stops being usable as a signal at all (PRD §6).
	Ambiguity judgment.NoulPolicy
	Consent   judgment.NoulPolicy
}{
	Version:         judgmentPolicyVersion,
	Route:           judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20},
	Server:          judgment.ChoicePolicy{MinTop: 0.80, MinMargin: 0.15, MinConfidence: 0.60},
	Transaction:     judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60},
	Category:        judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60},
	TransferPurpose: judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60},
	AmountSupport:   judgment.NoulPolicy{High: 0.85, Low: 0.15},
	DateSupport:     judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Ambiguity:       judgment.NoulPolicy{High: 0.15, Low: 0.05},
	Consent:         judgment.NoulPolicy{High: 0.85, Low: 0.15},
}

// judgmentCallMetric is one bounded-call observation. It is deliberately
// aggregate-only: task, protocol, model, status, and error class. No prompt,
// answer text, household identifier, or financial value is recorded here.
type judgmentCallMetric struct {
	Task       judgmentTask
	Model      string
	Status     string
	ErrorClass string
	DurationMs int64
	CallKind   string
}

// judgmentMetrics is the seam between the Telegram decision workflows and
// whatever records bounded-call telemetry. The zero value is a no-op recorder,
// so tests and unconfigured environments need no wiring.
type judgmentMetrics struct {
	Record func(context.Context, judgmentCallMetric)
	// Decision is incremented once per consumed decision with its product
	// outcome, which is what makes "review rate per decision task" computable.
	Decision func(context.Context, judgmentTask, judgmentOutcome)
}

func (m judgmentMetrics) recordCall(ctx context.Context, metric judgmentCallMetric) {
	if m.Record == nil {
		return
	}
	m.Record(ctx, metric)
}

func (m judgmentMetrics) recordDecision(ctx context.Context, task judgmentTask, outcome judgmentOutcome) {
	if m.Decision == nil {
		return
	}
	m.Decision(ctx, task, outcome)
}

// errorClassFrom reduces an error to a bounded, log-safe class so telemetry
// never carries provider messages, prompts, or user content.
func errorClassFrom(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "context deadline") || strings.Contains(text, "timeout"):
		return "TIMEOUT"
	case strings.Contains(text, "401") || strings.Contains(text, "403") || strings.Contains(text, "unauthorized"):
		return "AUTH"
	case strings.Contains(text, "429") || strings.Contains(text, "rate limit"):
		return "RATE_LIMITED"
	case strings.Contains(text, "status 5") || strings.Contains(text, "unavailable"):
		return "UPSTREAM"
	case strings.Contains(text, "decode") || strings.Contains(text, "invalid") || strings.Contains(text, "malformed"):
		return "MALFORMED"
	default:
		return "UNKNOWN"
	}
}

// JudgmentMetricsFor adapts the worker's llm_call recorder into the Telegram
// decision-plane metric seam. Bounded calls are reported as protocol
// `systemone` with the task name, and each consumed decision is reported as a
// zero-duration DECISION row carrying the product outcome. Only task, model,
// status, error class, and outcome are reported: no prompt, answer text, or
// financial value (PRD §16/§17).
//
// The turn context supplies the household so the row is attributable; the
// recorder is process-wide and would otherwise write household-less rows that
// no per-household scoreboard can read.
func JudgmentMetricsFor(record func(context.Context, gateway.CallMetric)) judgmentMetrics {
	return judgmentMetrics{
		Record: func(ctx context.Context, metric judgmentCallMetric) {
			record(metricCtx(ctx), gateway.CallMetric{
				Task:       string(metric.Task),
				Protocol:   "systemone",
				Model:      metric.Model,
				Status:     metric.Status,
				ErrorClass: metric.ErrorClass,
				DurationMs: metric.DurationMs,
				CallKind:   metric.CallKind,
			})
		},
		Decision: func(ctx context.Context, task judgmentTask, outcome judgmentOutcome) {
			// A decision row records what Go policy did with the answer. That
			// outcome is not a transport status, and llm_call.status only accepts
			// SUCCEEDED/FAILED, so the outcome travels in error_class (with the
			// bounded vocabulary in errorClassFrom for real failures) and the
			// status stays inside the contract.
			status := "SUCCEEDED"
			if outcome == judgmentOutcomeProviderFailure || outcome == judgmentOutcomeJudgmentUnavailable {
				status = "FAILED"
			}
			record(metricCtx(ctx), gateway.CallMetric{
				Task:       string(task),
				Protocol:   "systemone",
				Status:     status,
				ErrorClass: string(outcome),
				CallKind:   "DECISION",
			})
		},
	}
}

// metricCtx preserves the turn's household attribution without letting a
// cancelled turn drop the metric write.
func metricCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

// evaluate runs one bounded judgment call with task attribution. Every call goes
// through this wrapper so latency, status, and error class are recorded per
// decision task without touching prompt or answer content (PRD §17).
func (p *Processor) evaluate(ctx context.Context, task judgmentTask, requestID string, request judgment.Request) (judgment.Result, error) {
	started := time.Now()
	if p.judgment == nil {
		// Unconfigured judgment plane. Callers own the fail-closed policy; this
		// wrapper must never turn "not configured" into a nil dereference.
		p.metrics.recordCall(ctx, judgmentCallMetric{Task: task, Status: "FAILED", ErrorClass: "UNCONFIGURED", CallKind: "JUDGMENT"})
		return judgment.Result{}, errJudgmentUnavailable
	}
	result, err := p.judgment.Evaluate(ctx, requestID, request)
	// Track which bounded tasks this turn consumed for turn-level value
	// telemetry (PRD §23). The trace rides in the turn context, so concurrent
	// turns never share it. Only a call that actually answered counts: a provider
	// failure is not a consumed Jev decision and must not read as one when the
	// turn lane is classified. Only the task name and model are kept.
	if trace := turnTraceFrom(ctx); trace != nil && err == nil {
		trace.record(task, result.Model)
	}
	// The gateway reports SUCCEEDED/FAILED and llm_call enforces that contract;
	// a bounded call must not invent a third vocabulary or its rows are rejected.
	status := "SUCCEEDED"
	if err != nil {
		status = "FAILED"
	}
	p.metrics.recordCall(ctx, judgmentCallMetric{
		Task:       task,
		Model:      result.Model,
		Status:     status,
		ErrorClass: errorClassFrom(err),
		DurationMs: time.Since(started).Milliseconds(),
		CallKind:   "JUDGMENT",
	})
	return result, err
}
