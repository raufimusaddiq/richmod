package operations

import "context"

type judgmentAggregate struct {
	WindowDays int            `json:"windowDays"`
	Turns      map[string]int `json:"turns"`
	Avoided    int            `json:"nativeToolCallsAvoided"`
	Decisions  map[string]any `json:"decisions"`
	Phases     map[string]any `json:"phases"`
}

func (h *Handler) loadJudgmentAggregate(ctx context.Context, householdID string) (judgmentAggregate, error) {
	aggregate := judgmentAggregate{WindowDays: 30, Turns: map[string]int{}, Decisions: map[string]any{}, Phases: map[string]any{}}

	err := h.runProbes(ctx,
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `
				SELECT lane, count(*), coalesce(sum(native_tool_calls_avoided), 0)
				FROM judgment_turn_telemetry
				WHERE household_id=$1 AND created_at >= now() - interval '30 days'
				GROUP BY lane`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var lane string
				var count, avoided int
				if err := rows.Scan(&lane, &count, &avoided); err != nil {
					return err
				}
				aggregate.Turns[lane] = count
				aggregate.Avoided += avoided
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			var calls, failures int
			var latencyP50 float64
			if err := h.pool.QueryRow(ctx, `
				SELECT count(*) FILTER (WHERE call_kind='JUDGMENT'),
				 count(*) FILTER (WHERE call_kind='JUDGMENT' AND status='FAILED'),
				 coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE call_kind='JUDGMENT'), 0)
				FROM llm_call
				WHERE household_id=$1 AND protocol='systemone' AND created_at >= now() - interval '30 days'`,
				householdID).Scan(&calls, &failures, &latencyP50); err != nil {
				return err
			}
			aggregate.Decisions = map[string]any{
				"judgmentCalls":      calls,
				"judgmentFailures":   failures,
				"judgmentLatencyP50": latencyP50,
			}
			return nil
		},
		func(ctx context.Context) error {
			var jevCalls, generativeCalls, events, residualAttempts, residualSuccess, redundant, mixed int
			var phaseP50, phaseP95 float64
			if err := h.pool.QueryRow(ctx, `
				WITH recent AS (SELECT * FROM intelligence_phase_telemetry WHERE household_id=$1 AND created_at >= now()-interval '30 days'), per_event AS (
				SELECT source_event_id,count(*) FILTER(WHERE capability='JEV') jev,count(*) FILTER(WHERE capability='GENERATIVE') generative,
				bool_or(capability='GENERATIVE' AND 'category'=ANY(answered_dimensions)) generated_category,
				bool_or(capability='JEV' AND 'category'=ANY(semantic_dimensions) AND purpose<>'RESIDUAL_CATEGORY') repeated_category
				FROM recent WHERE source_event_id IS NOT NULL GROUP BY source_event_id)
				SELECT count(*) FILTER(WHERE capability='JEV'),count(*) FILTER(WHERE capability='GENERATIVE'),count(DISTINCT source_event_id),
				coalesce(percentile_cont(.5) within group(order by latency_ms),0),coalesce(percentile_cont(.95) within group(order by latency_ms),0),
				(SELECT count(*) FROM recent WHERE purpose='RESIDUAL_CATEGORY' AND capability='JEV'),
				(SELECT count(DISTINCT p.id) FROM recent p JOIN judgment_decision d ON d.source_event_id=p.source_event_id WHERE p.purpose='RESIDUAL_CATEGORY' AND p.capability='JEV' AND d.policy_version IS NOT NULL AND d.outcome IN ('AUTO_CONFIRM','CONFIRMED') AND d.created_at BETWEEN p.created_at-interval '1 minute' AND p.created_at+interval '1 minute'),
				(SELECT count(*) FROM per_event WHERE generated_category AND repeated_category),
				(SELECT count(*) FROM per_event WHERE generative>0 AND jev>0) FROM recent`, householdID).Scan(&jevCalls, &generativeCalls, &events, &phaseP50, &phaseP95, &residualAttempts, &residualSuccess, &redundant, &mixed); err != nil {
				return err
			}
			aggregate.Phases = map[string]any{"jevCalls": jevCalls, "generativeCalls": generativeCalls, "events": events, "passes": jevCalls + generativeCalls, "residualCategoryPasses": residualAttempts, "residualRescueSuccesses": residualSuccess, "categoryDoublePassCandidates": redundant, "mixedModelEvents": mixed, "latencyP50Ms": phaseP50, "latencyP95Ms": phaseP95}
			return nil
		},
	)
	return aggregate, err
}
