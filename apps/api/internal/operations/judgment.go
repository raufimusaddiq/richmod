package operations

import "context"

// judgmentAggregate is the PRD §23 "is Jev actually saving work" scoreboard:
// how many Telegram turns each decision lane resolved, and how many bounded
// generative tool calls the decision plane made unnecessary. It is computed on
// read from the two tables the worker already writes, so no new pipeline exists
// to keep in sync and no history is lost when thresholds change.
//
// ponytail: unbounded 30-day scan. Add a materialized rollup when the table
// outgrows a cheap aggregate, which at one row per Telegram turn is a long way.
type judgmentAggregate struct {
	WindowDays int            `json:"windowDays"`
	Turns      map[string]int `json:"turns"`
	Avoided    int            `json:"nativeToolCallsAvoided"`
	Decisions  map[string]any `json:"decisions"`
}

func (h *Handler) loadJudgmentAggregate(ctx context.Context, householdID string) (judgmentAggregate, error) {
	aggregate := judgmentAggregate{WindowDays: 30, Turns: map[string]int{}, Decisions: map[string]any{}}
	rows, err := h.pool.Query(ctx, `
		SELECT lane, count(*), coalesce(sum(native_tool_calls_avoided), 0)
		FROM judgment_turn_telemetry
		WHERE household_id=$1 AND created_at >= now() - interval '30 days'
		GROUP BY lane`, householdID)
	if err != nil {
		return aggregate, err
	}
	defer rows.Close()
	for rows.Next() {
		var lane string
		var count, avoided int
		if err := rows.Scan(&lane, &count, &avoided); err != nil {
			return aggregate, err
		}
		aggregate.Turns[lane] = count
		aggregate.Avoided += avoided
	}
	if err := rows.Err(); err != nil {
		return aggregate, err
	}

	// Jev-vs-generative call share, latency, and whether the decision plane is
	// failing: every bounded call and consumed decision lands in llm_call as
	// protocol 'systemone'. Only JUDGMENT rows are transport calls, so failures
	// are counted there (a DECISION row's status is SUCCEEDED/FAILED, not the
	// outcome, which travels in error_class).
	var calls, failures int
	var latencyP50 float64
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE call_kind='JUDGMENT'),
		 count(*) FILTER (WHERE call_kind='JUDGMENT' AND status='FAILED'),
		 coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE call_kind='JUDGMENT'), 0)
		FROM llm_call
		WHERE household_id=$1 AND protocol='systemone' AND created_at >= now() - interval '30 days'`,
		householdID).Scan(&calls, &failures, &latencyP50); err != nil {
		return aggregate, err
	}
	aggregate.Decisions = map[string]any{
		"judgmentCalls":      calls,
		"judgmentFailures":   failures,
		"judgmentLatencyP50": latencyP50,
	}
	return aggregate, nil
}
