package telegram

import "context"

// turnTrace accumulates the bounded decision tasks one turn consumed, plus the
// model that answered them. It is carried in the turn's context rather than on
// the Processor, because one Processor serves concurrent Telegram turns
// (WORKER_CHAT_CONCURRENCY) and shared mutable state would race between them.
type turnTrace struct {
	tasks              []string
	model              string
	residualDimensions []string
	// householdID lets the bounded-call recorder attribute llm_call rows to a
	// household, which is what makes the per-household value aggregate possible.
	// The gateway recorder is process-wide and sees no turn state otherwise.
	householdID string
}

type turnTraceKey struct{}

// withTurnTrace attaches a fresh trace to a turn's context.
func withTurnTrace(ctx context.Context) (context.Context, *turnTrace) {
	trace := &turnTrace{}
	return context.WithValue(ctx, turnTraceKey{}, trace), trace
}

// turnTraceFrom returns the trace attached to the context, or nil when the
// context has none (tests, non-turn callers).
func turnTraceFrom(ctx context.Context) *turnTrace {
	trace, _ := ctx.Value(turnTraceKey{}).(*turnTrace)
	return trace
}

// TurnHouseholdID returns the household a turn's context belongs to, or "" when
// the caller is not a Telegram turn (tests, background jobs). The worker's
// process-wide metric recorder calls it so a bounded-call row is attributable.
func TurnHouseholdID(ctx context.Context) string {
	if trace := turnTraceFrom(ctx); trace != nil {
		return trace.householdID
	}
	return ""
}

func (t *turnTrace) record(task judgmentTask, model string) {
	for _, existing := range t.tasks {
		if existing == string(task) {
			if model != "" {
				t.model = model
			}
			return
		}
	}
	t.tasks = append(t.tasks, string(task))
	if model != "" {
		t.model = model
	}
}

func (t *turnTrace) recordResidual(dimensions []string) {
	if t == nil || len(dimensions) == 0 {
		return
	}
	t.residualDimensions = append([]string(nil), dimensions...)
}

func (t turnTrace) consumed() bool { return len(t.tasks) > 0 }

// judgmentTurnLane classifies how a Telegram turn was resolved, which is what
// makes the Jev value claim measurable: a turn the bounded decision plane
// answered alone spends no generative call, and that is the thing worth
// counting (PRD §23).
type judgmentTurnLane string

const (
	judgmentLaneJevOnly           judgmentTurnLane = "JEV_ONLY"
	judgmentLaneJevThenGenerative judgmentTurnLane = "JEV_THEN_GENERATIVE"
	judgmentLaneResidualJev       judgmentTurnLane = "JEV_THEN_GENERATIVE_THEN_RESIDUAL_JEV"
	judgmentLaneGenerativeOnly    judgmentTurnLane = "GENERATIVE_ONLY"
)

// judgmentTurnObservation is one turn's value measurement: aggregate-only lane,
// which bounded tasks ran, the policy/model that answered them, and how many
// generative tool calls the bounded plane made unnecessary. No prompt, answer
// text, household message, or financial value is recorded.
type judgmentTurnObservation struct {
	Lane                   judgmentTurnLane
	DecisionTasks          []string
	ResidualDimensions     []string
	Model                  string
	NativeToolCallsAvoided int
}

// SetTurnTelemetry enables turn-level value recording (PRD §23). Disabled keeps
// the previous behaviour: no rows are written.
func (p *Processor) SetTurnTelemetry(enabled bool) { p.turnTelemetryEnabled = enabled }

func (p *Processor) recordTurnTelemetry(ctx context.Context, householdID, sourceEventID string, observation judgmentTurnObservation) {
	if !p.turnTelemetryEnabled || p.pool == nil {
		return
	}
	if observation.DecisionTasks == nil {
		observation.DecisionTasks = []string{}
	}
	if observation.ResidualDimensions == nil {
		observation.ResidualDimensions = []string{}
	}
	if observation.NativeToolCallsAvoided < 0 {
		observation.NativeToolCallsAvoided = 0
	}
	_, _ = p.pool.Exec(ctx, `INSERT INTO judgment_turn_telemetry(household_id,source_event_id,lane,decision_tasks,residual_dimensions,policy_version,model,native_tool_calls_avoided) VALUES(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,NULLIF($7,''),$8)`, householdID, sourceEventID, string(observation.Lane), observation.DecisionTasks, observation.ResidualDimensions, judgmentPolicyVersion, observation.Model, observation.NativeToolCallsAvoided)
}
