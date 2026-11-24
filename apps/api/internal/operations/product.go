package operations

import (
	"context"
)

type productAggregate struct {
	WindowDays     int            `json:"windowDays"`
	SourceEvents   int            `json:"sourceEvents"`
	Processed      int            `json:"processed"`
	Ignored        int            `json:"ignored"`
	NeedsReview    int            `json:"needsReview"`
	ReviewedEvents int            `json:"reviewedEvents"`
	HumanTouchRate float64        `json:"humanTouchRate"`
	BySource       map[string]int `json:"sourceEventsBySource"`
	ReviewBySource map[string]int `json:"reviewRateBySource"`
	ReviewByReason map[string]int `json:"reviewRateByReason"`

	CanonicalEvents             int            `json:"canonicalEvents"`
	ExplicitInputs              int            `json:"explicitInputs"`
	RHICE                       float64        `json:"rhice"`
	TypedFields                 int            `json:"typedFields"`
	OpenReviews                 int            `json:"openReviews"`
	AcceptedWithoutEdit         int            `json:"reviewAcceptedWithoutEdit"`
	Coverage                    []string       `json:"notYetMeasurable"`
	CoverageIncomplete          []string       `json:"coverageIncomplete"`
	TimeToResolutionMs          int64          `json:"timeToResolutionMs"`
	ReviewRoundTrips            int            `json:"reviewRoundTrips"`
	BoundedChoices              int            `json:"boundedChoices"`
	BoundedChoicesPerEvent      float64        `json:"boundedChoicesPerEvent"`
	AutoConfirmCorrections      int            `json:"autoConfirmCorrections"`
	AutoConfirmEvents           int            `json:"autoConfirmEvents"`
	AutoConfirmCorrectionRate   float64        `json:"autoConfirmCorrectionRate"`
	AutoConfirmCorrectionFields map[string]int `json:"autoConfirmCorrectionFields"`
	AutoConfirmCorrectionSource map[string]int `json:"autoConfirmCorrectionBySource"`
	KnownFactReasks             int            `json:"knownFactReasks"`
	ReviewsWithDecision         int            `json:"reviewsWithDecision"`
	KnownFactReaskRate          float64        `json:"knownFactReaskRate"`
	ValidatorEligibleReviews    int            `json:"validatorEligibleReviews"`
	ValidatorInducedReviews     int            `json:"validatorInducedReviews"`
	ValidatorUnknownReviews     int            `json:"validatorUnknownReviews"`
	ValidatorInducedRate    float64        `json:"validatorInducedRate"`
	ResidualEligibleReviews int            `json:"residualEligibleReviews"`
	ResidualViolations          int            `json:"residualViolations"`
	ResidualUnknownReviews      int            `json:"residualUnknownReviews"`
	ResidualFidelityRate        float64        `json:"residualFidelityRate"`
	CEUBinding                  map[string]int `json:"ceuBinding"`
}

func (h *Handler) loadProductAggregate(ctx context.Context, householdID string) (productAggregate, error) {
	aggregate := productAggregate{WindowDays: 30, CEUBinding: map[string]int{}, BySource: map[string]int{}, ReviewBySource: map[string]int{}, ReviewByReason: map[string]int{}, AutoConfirmCorrectionFields: map[string]int{}, AutoConfirmCorrectionSource: map[string]int{}}

	var boundedChoicesPerEventSum float64
	var telemetryHistoryComplete bool

	err := h.runProbes(ctx,
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				SELECT count(*),
				 count(*) FILTER (WHERE processing_status='PROCESSED'),
				 count(*) FILTER (WHERE processing_status='IGNORED'),
				 count(*) FILTER (WHERE processing_status='NEEDS_REVIEW'),
				 count(DISTINCT se.id) FILTER (WHERE EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id=se.id))
				FROM source_event se
				WHERE se.household_id=$1 AND se.received_at >= now() - interval '30 days'`, householdID).
				Scan(&aggregate.SourceEvents, &aggregate.Processed, &aggregate.Ignored, &aggregate.NeedsReview, &aggregate.ReviewedEvents)
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `
				SELECT source_type, count(*)
				FROM source_event
				WHERE household_id=$1 AND received_at >= now() - interval '30 days'
				GROUP BY source_type`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var sourceType string
				var count int
				if err := rows.Scan(&sourceType, &count); err != nil {
					return err
				}
				aggregate.BySource[sourceType] = count
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `
				SELECT COALESCE(se.source_type,'unattributed') AS source_type, count(*)
				FROM review_item ri
				LEFT JOIN source_event se ON se.id=ri.source_event_id
				WHERE ri.household_id=$1 AND ri.created_at >= now() - interval '30 days'
				GROUP BY 1`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var sourceType string
				var count int
				if err := rows.Scan(&sourceType, &count); err != nil {
					return err
				}
				aggregate.ReviewBySource[sourceType] = count
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `
				SELECT review_type, count(*)
				FROM review_item
				WHERE household_id=$1 AND created_at >= now() - interval '30 days'
				GROUP BY review_type`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var reviewType string
				var count int
				if err := rows.Scan(&reviewType, &count); err != nil {
					return err
				}
				aggregate.ReviewByReason[reviewType] = count
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				SELECT count(*), count(*) FILTER (WHERE EXISTS (
					SELECT 1 FROM jsonb_array_elements_text(coalesce(ri.decision->'missingFacts','[]'::jsonb)) AS missing(fact)
					WHERE ri.decision->'knownFacts' ? missing.fact
					  AND ri.decision->'knownFacts'->missing.fact <> 'null'::jsonb
				)) FROM review_item ri
				WHERE ri.household_id=$1 AND ri.created_at >= now()-interval '30 days'
				  AND jsonb_typeof(ri.decision->'missingFacts')='array'`, householdID).
				Scan(&aggregate.ReviewsWithDecision, &aggregate.KnownFactReasks)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				WITH reviews AS (
				  SELECT decision,
				    jsonb_typeof(decision#>'{decisionProvenance,accepted_dimensions_at_validation}')='array'
				      AND jsonb_typeof(decision->'affectedFacts')='array'
				      AND jsonb_typeof(decision->'missingFacts')='array'
				      AND decision->>'validationConsequence' IN ('BOUNDED_RESIDUAL','REPRESENTATION_INVALID') AS eligible
				  FROM review_item WHERE household_id=$1 AND created_at>=now()-interval '30 days'
				    AND (decision->>'decisionClass'='EVIDENCE_GAP' OR decision IS NULL)
				)
				SELECT count(*) FILTER (WHERE eligible),
				  count(*) FILTER (WHERE eligible AND EXISTS (
				    SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(decision->'affectedFacts')='array' THEN decision->'affectedFacts' ELSE '[]'::jsonb END) a(fact)
				    WHERE decision->'missingFacts' ? a.fact
				      AND decision#>'{decisionProvenance,accepted_dimensions_at_validation}' ? a.fact
				  )),
				  count(*) FILTER (WHERE NOT COALESCE(eligible,false))
				FROM reviews`, householdID).Scan(&aggregate.ValidatorEligibleReviews, &aggregate.ValidatorInducedReviews, &aggregate.ValidatorUnknownReviews)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				WITH contracts AS (
				  SELECT ri.id,ri.review_type,ri.status,ri.resolution_action,ri.decision,
				    jsonb_typeof(ri.decision->'knownFacts')='object'
				      AND jsonb_typeof(ri.decision->'missingFacts')='array'
				      AND COALESCE(ri.decision->>'reasonCode','')<>''
				      AND COALESCE(ri.decision->>'decisionClass','')<>''
				      AND COALESCE(ri.decision->>'whyNotAutoConfirm','')<>'' AS eligible
				  FROM review_item ri WHERE ri.household_id=$1 AND ri.created_at>=now()-interval '30 days'
				), scored AS (
				 SELECT eligible,
				   COALESCE(eligible,false) AND (
				     EXISTS (SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(decision->'missingFacts')='array' THEN decision->'missingFacts' ELSE '[]'::jsonb END) m(fact)
				       WHERE decision->'knownFacts' ? m.fact AND decision->'knownFacts'->m.fact <> 'null'::jsonb)
				     OR (decision->>'validationConsequence'='QUALITY_SIGNAL' AND jsonb_array_length(CASE WHEN jsonb_typeof(decision->'missingFacts')='array' THEN decision->'missingFacts' ELSE '[]'::jsonb END)>0)
				     OR (resolution_action IN ('SET_PAY_DATE','SET_TRANSACTION_DATE') AND NOT (decision->'missingFacts' ? 'transaction_at'))
				     OR (resolution_action='SET_FINANCIAL_EMAIL_ENTITIES' AND NOT (decision->'missingFacts' ?| ARRAY['funding_account','wealth_account']))
				     OR (review_type<>'MANUAL_CORRECTION' AND COALESCE(resolution_action,'') NOT IN ('EDIT','CORRECT')
				       AND EXISTS (SELECT 1 FROM product_telemetry_event e CROSS JOIN LATERAL unnest(e.changed_fields) f(field)
				         WHERE e.review_item_id=contracts.id AND e.event_type='REVIEW_TURN'
				           AND NOT (decision->'missingFacts' ? CASE f.field WHEN 'amount_idr' THEN 'amount' WHEN 'account' THEN 'funding_account' ELSE f.field END)))
				   ) AS violation FROM contracts
				)
				SELECT count(*) FILTER (WHERE eligible),count(*) FILTER (WHERE violation),
				  count(*) FILTER (WHERE NOT COALESCE(eligible,false)) FROM scored`, householdID).
				Scan(&aggregate.ResidualEligibleReviews, &aggregate.ResidualViolations, &aggregate.ResidualUnknownReviews)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				WITH recent_transactions AS (
				  SELECT id FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND created_at >= now() - interval '30 days'
				), stale_transactions AS (
				  SELECT DISTINCT transaction_id FROM review_item
				  WHERE household_id=$1 AND transaction_id IS NOT NULL AND status='RESOLVED'
				    AND resolved_at < now() - interval '30 days'
				), review_events AS (
				  SELECT DISTINCT ri.id, ri.resolution_action
				  FROM review_item ri
				  WHERE ri.household_id=$1 AND ri.status='RESOLVED' AND ri.resolved_at >= now() - interval '30 days'
				    AND (ri.transaction_id IS NULL OR ri.transaction_id NOT IN (SELECT transaction_id FROM stale_transactions))
				    AND (
				      ri.transaction_id IN (SELECT id FROM recent_transactions)
				      OR (ri.resolution_action='MERGE_REVIEW' AND ri.resolution_values->>'transaction_id' IN (SELECT id::text FROM recent_transactions))
				      OR EXISTS (
				        SELECT 1 FROM transaction_evidence te
				        WHERE te.transaction_id IN (SELECT id FROM recent_transactions)
				          AND (
				            (ri.financial_email_observation_id IS NOT NULL AND EXISTS (SELECT 1 FROM financial_email_observation feo WHERE feo.id=ri.financial_email_observation_id AND feo.transaction_id=te.transaction_id))
				            OR (ri.wealth_observation_id IS NOT NULL AND te.metadata_json->>'observation_id'=ri.wealth_observation_id::text)
				            OR (ri.financial_email_observation_id IS NULL AND te.source_event_id=COALESCE(ri.source_event_id, (SELECT source_event_id FROM transaction_proposal WHERE id=ri.proposal_id), (SELECT source_event_id FROM document WHERE id=ri.document_id)))
				          )
				      )
				    )
				), human_turns AS (
				  SELECT COALESCE(sum(cardinality(e.changed_fields)+e.bounded_choices),0) AS inputs,
				         COALESCE(sum(cardinality(e.changed_fields)),0) AS typed_fields
				  FROM product_telemetry_event e
				  JOIN review_events re ON re.id=e.review_item_id
				  WHERE e.household_id=$1 AND e.event_type='REVIEW_TURN' AND e.occurred_at >= now() - interval '30 days'
				    AND e.action NOT IN ('IGNORE','EMAIL_RECEIVED_AT_FALLBACK','RECONCILED_TERMINAL_TRANSACTION','LEGACY_TRANSACTION_RESOLVED','NO_LONGER_APPLICABLE')
				)
				SELECT
				 (SELECT inputs FROM human_turns),
				 (SELECT typed_fields FROM human_turns),
				 (SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')),
				 count(*) FILTER (WHERE resolution_action IN (
				   'CONFIRM_REVIEW','TELEGRAM_CONFIRMED','TELEGRAM_MERCHANT_DECISION'))
				FROM review_events`, householdID).Scan(&aggregate.ExplicitInputs, &aggregate.TypedFields, &aggregate.OpenReviews, &aggregate.AcceptedWithoutEdit)
		},
		func(ctx context.Context) error {
			if err := h.pool.QueryRow(ctx, `
				SELECT count(*) FROM transaction
				WHERE household_id=$1 AND status='CONFIRMED' AND created_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.CanonicalEvents); err != nil {
				return err
			}
			if aggregate.CanonicalEvents == 0 {
				return nil
			}
			return h.pool.QueryRow(ctx, `
				SELECT COALESCE(sum(e.bounded_choices),0)
				FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id
				WHERE e.household_id=$1 AND e.event_type='REVIEW_TURN'
				  AND t.status='CONFIRMED' AND t.created_at >= now()-interval '30 days'`, householdID).Scan(&boundedChoicesPerEventSum)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				SELECT COALESCE(avg(EXTRACT(EPOCH FROM (ri.resolved_at - ri.created_at)) * 1000)::bigint, 0)
				FROM review_item ri
				WHERE ri.household_id=$1 AND ri.created_at >= now() - interval '30 days' AND ri.status='RESOLVED'`, householdID).Scan(&aggregate.TimeToResolutionMs)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				SELECT count(*) FILTER (WHERE event_type='REVIEW_TURN'),COALESCE(sum(bounded_choices) FILTER (WHERE event_type='REVIEW_TURN'),0)
				FROM product_telemetry_event WHERE household_id=$1 AND occurred_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.ReviewRoundTrips, &aggregate.BoundedChoices)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				SELECT count(DISTINCT e.transaction_id)
				FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id
				WHERE e.household_id=$1 AND e.event_type='AUTO_CONFIRM_CORRECTION' AND t.auto_confirmed_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.AutoConfirmCorrections)
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND auto_confirmed_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.AutoConfirmEvents)
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `SELECT action,count(*) FROM product_telemetry_event WHERE household_id=$1 AND event_type='CEU_BINDING' AND occurred_at >= now()-interval '30 days' GROUP BY action`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var key string
				var count int
				if err := rows.Scan(&key, &count); err != nil {
					return err
				}
				aggregate.CEUBinding[key] = count
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `SELECT field,count(*) FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id CROSS JOIN LATERAL unnest(e.changed_fields) AS changed(field) WHERE e.household_id=$1 AND e.event_type='AUTO_CONFIRM_CORRECTION' AND t.auto_confirmed_at >= now()-interval '30 days' GROUP BY field`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var key string
				var count int
				if err := rows.Scan(&key, &count); err != nil {
					return err
				}
				aggregate.AutoConfirmCorrectionFields[key] = count
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `SELECT COALESCE(e.source_type,'unknown'),count(*) FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id WHERE e.household_id=$1 AND e.event_type='AUTO_CONFIRM_CORRECTION' AND t.auto_confirmed_at >= now()-interval '30 days' GROUP BY 1`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var key string
				var count int
				if err := rows.Scan(&key, &count); err != nil {
					return err
				}
				aggregate.AutoConfirmCorrectionSource[key] = count
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `SELECT COALESCE(min(occurred_at),now()) <= now()-interval '30 days' FROM product_telemetry_event WHERE household_id=$1 AND event_type<>'UI_EVENT'`, householdID).Scan(&telemetryHistoryComplete)
		},
	)

	if aggregate.SourceEvents > 0 {
		aggregate.HumanTouchRate = float64(aggregate.ReviewedEvents) / float64(aggregate.SourceEvents)
	}
	if aggregate.ReviewsWithDecision > 0 {
		aggregate.KnownFactReaskRate = float64(aggregate.KnownFactReasks) / float64(aggregate.ReviewsWithDecision)
	}
	if aggregate.ValidatorEligibleReviews > 0 {
		aggregate.ValidatorInducedRate = float64(aggregate.ValidatorInducedReviews) / float64(aggregate.ValidatorEligibleReviews)
	}
	if aggregate.ResidualEligibleReviews > 0 {
		aggregate.ResidualFidelityRate = float64(aggregate.ResidualEligibleReviews-aggregate.ResidualViolations) / float64(aggregate.ResidualEligibleReviews)
	}
	if aggregate.CanonicalEvents > 0 {
		aggregate.RHICE = float64(aggregate.ExplicitInputs) / float64(aggregate.CanonicalEvents)
		aggregate.BoundedChoicesPerEvent = boundedChoicesPerEventSum / float64(aggregate.CanonicalEvents)
	}
	if aggregate.AutoConfirmEvents > 0 {
		aggregate.AutoConfirmCorrectionRate = float64(aggregate.AutoConfirmCorrections) / float64(aggregate.AutoConfirmEvents)
	}
	aggregate.Coverage = []string{}
	if !telemetryHistoryComplete {
		aggregate.Coverage = append(aggregate.Coverage, "pre_migration_telemetry_history")
	}
	if aggregate.ValidatorUnknownReviews > 0 {
		aggregate.CoverageIncomplete = append(aggregate.CoverageIncomplete, "validator_induced_review_provenance")
	}
	if aggregate.ResidualUnknownReviews > 0 {
		aggregate.CoverageIncomplete = append(aggregate.CoverageIncomplete, "residual_contract_history")
	}
	return aggregate, err
}
