package telegram

import "context"

// turnTrace accumulates the bounded decision tasks one turn consumed, plus the
// model that answered them. A single Telegram turn is handled by one goroutine,
// so the field needs no synchronization.
type turnTrace struct {
	tasks []string
	model string
}

func (t *turnTrace) reset() { t.tasks = nil; t.model = "" }

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

func (t turnTrace) consumed() bool { return len(t.tasks) > 0 }

// judgmentTurnLane classifies how a Telegram turn was resolved, which is what
// makes the Jev value claim measurable: a turn the bounded decision plane
// answered alone spends no generative call, and that is the thing worth
// counting (PRD §23).
type judgmentTurnLane string

const (
	judgmentLaneJevOnly           judgmentTurnLane = "JEV_ONLY"
	judgmentLaneJevThenGenerative judgmentTurnLane = "JEV_THEN_GENERATIVE"
	judgmentLaneGenerativeOnly    judgmentTurnLane = "GENERATIVE_ONLY"
)

// judgmentTurnObservation is one turn's value measurement: aggregate-only lane,
// which bounded tasks ran, the policy/model that answered them, and how many
// generative tool calls the bounded plane made unnecessary. No prompt, answer
// text, household message, or financial value is recorded.
type judgmentTurnObservation struct {
	Lane                   judgmentTurnLane
	DecisionTasks          []string
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
	if observation.NativeToolCallsAvoided < 0 {
		observation.NativeToolCallsAvoided = 0
	}
	_, _ = p.pool.Exec(ctx, `INSERT INTO judgment_turn_telemetry(household_id,source_event_id,lane,decision_tasks,policy_version,model,native_tool_calls_avoided) VALUES(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4,$5,NULLIF($6,''),$7)`, householdID, sourceEventID, string(observation.Lane), observation.DecisionTasks, judgmentPolicyVersion, observation.Model, observation.NativeToolCallsAvoided)
}
