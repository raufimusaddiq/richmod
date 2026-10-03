package operations

import (
	"context"
)

// productAggregate is the PRD §22 product scoreboard computed from canonical
// state that already exists: source events, transactions, and review items. It
// is read-only and stores nothing, so it cannot drift from the ledger and needs
// no new pipeline. Amounts and free text are never selected.
//
// Every metric below is derived from canonical state, so the aggregate cannot
// disagree with the ledger and needs no write-path instrumentation.
//
// ponytail: unbounded 30-day scans with no new indexes. Add a rollup table when
// these tables outgrow a cheap aggregate, not before.
type productAggregate struct {
	WindowDays int `json:"windowDays"`
	// SourceEvents is raw ingestion in the window and is the cohort for the rate
	// below. Processed / Ignored / NeedsReview are the source-event *processing*
	// states, named as such because IGNORED (a promo, an internal move) is not a
	// canonical financial event. HumanTouchRate uses the same population for its
	// numerator and denominator — distinct events in the window that carry a
	// review, over all events in the window — so it cannot exceed 1.
	SourceEvents   int            `json:"sourceEvents"`
	Processed      int            `json:"processed"`
	Ignored        int            `json:"ignored"`
	NeedsReview    int            `json:"needsReview"`
	ReviewedEvents int            `json:"reviewedEvents"`
	HumanTouchRate float64        `json:"humanTouchRate"`
	BySource       map[string]int `json:"sourceEventsBySource"`
	ReviewBySource map[string]int `json:"reviewRateBySource"`
	ReviewByReason map[string]int `json:"reviewRateByReason"`

	// RHICE is the PRD 2.2 north-star metric: explicit human inputs required before
	// each canonical financial event reached a valid canonical state, divided by
	// the number of canonical financial events. The numerator is the recorded
	// review turns, so a form that submits several fields counts each supplied
	// field rather than one action name; a canonical event is one transaction.
	// Derived, not written, so it cannot drift from the ledger.
	CanonicalEvents int     `json:"canonicalEvents"`
	ExplicitInputs  int     `json:"explicitInputs"`
	RHICE           float64 `json:"rhice"`
	TypedFields     int     `json:"typedFields"`
	// Reviews still open is the friction the proposal-first card targets.
	OpenReviews int `json:"openReviews"`
	// AcceptedWithoutEdit counts resolutions that only accepted a proposal:
	// CONFIRM_REVIEW (web) and its Telegram equivalents. MERGE_EXISTING is a
	// choice between candidates, not an accepted proposal, so it is excluded.
	AcceptedWithoutEdit int `json:"reviewAcceptedWithoutEdit"`
	// Permanent coverage gaps are distinct from incomplete historical provenance.
	Coverage           []string `json:"notYetMeasurable"`
	CoverageIncomplete []string `json:"coverageIncomplete"`
	// TimeToResolutionMs is the mean wall-clock time from review open to resolve
	// for the reviews in the window, read from review_item so every review counts.
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
	ValidatorInducedRate        float64        `json:"validatorInducedRate"`
	SemanticEligiblePhases      int            `json:"semanticEligiblePhases"`
	SemanticReDecisions         int            `json:"semanticReDecisions"`
	SemanticUnknownPhases       int            `json:"semanticUnknownPhases"`
	SemanticReDecisionRate      float64        `json:"semanticReDecisionRate"`
	ResidualEligibleReviews     int            `json:"residualEligibleReviews"`
	ResidualViolations          int            `json:"residualViolations"`
	ResidualUnknownReviews      int            `json:"residualUnknownReviews"`
	ResidualFidelityRate        float64        `json:"residualFidelityRate"`
	// CEUBinding counts conversational-evidence binding outcomes in the window by
	// bounded action name (EXACT_REPLY_BINDING, RECENT_CONTEXT_BINDING,
	// AMBIGUOUS_CONTEXT, REFERENCE_EXPIRED, ...). Counters only: no text, value, or
	// identifier is stored, and rows from before CEU simply do not exist.
	CEUBinding map[string]int `json:"ceuBinding"`
}

func (h *Handler) loadProductAggregate(ctx context.Context, householdID string) (productAggregate, error) {
	aggregate := productAggregate{WindowDays: 30, CEUBinding: map[string]int{}, BySource: map[string]int{}, ReviewBySource: map[string]int{}, ReviewByReason: map[string]int{}, AutoConfirmCorrectionFields: map[string]int{}, AutoConfirmCorrectionSource: map[string]int{}}

	// Source-event processing states in the window, plus the distinct reviewed
	// events counted over the exact same cohort so the human-touch ratio is
	// internally consistent and bounded by 1.
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*),
		 count(*) FILTER (WHERE processing_status='PROCESSED'),
		 count(*) FILTER (WHERE processing_status='IGNORED'),
		 count(*) FILTER (WHERE processing_status='NEEDS_REVIEW'),
		 count(DISTINCT se.id) FILTER (WHERE EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id=se.id))
		FROM source_event se
		WHERE se.household_id=$1 AND se.received_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.SourceEvents, &aggregate.Processed, &aggregate.Ignored, &aggregate.NeedsReview, &aggregate.ReviewedEvents); err != nil {
		return aggregate, err
	}

	rows, err := h.pool.Query(ctx, `
		SELECT source_type, count(*)
		FROM source_event
		WHERE household_id=$1 AND received_at >= now() - interval '30 days'
		GROUP BY source_type`, householdID)
	if err != nil {
		return aggregate, err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceType string
		var count int
		if err := rows.Scan(&sourceType, &count); err != nil {
			return aggregate, err
		}
		aggregate.BySource[sourceType] = count
	}
	if err := rows.Err(); err != nil {
		return aggregate, err
	}

	// Review rate by source: review rows attributed to their originating source
	// type. A review with no source event (legacy/proposal-scoped) is grouped as
	// "unattributed" instead of dropped, so the total stays honest.
	reviewRows, err := h.pool.Query(ctx, `
		SELECT COALESCE(se.source_type,'unattributed') AS source_type, count(*)
		FROM review_item ri
		LEFT JOIN source_event se ON se.id=ri.source_event_id
		WHERE ri.household_id=$1 AND ri.created_at >= now() - interval '30 days'
		GROUP BY 1`, householdID)
	if err != nil {
		return aggregate, err
	}
	defer reviewRows.Close()
	for reviewRows.Next() {
		var sourceType string
		var count int
		if err := reviewRows.Scan(&sourceType, &count); err != nil {
			return aggregate, err
		}
		aggregate.ReviewBySource[sourceType] = count
	}
	if err := reviewRows.Err(); err != nil {
		return aggregate, err
	}

	reasonRows, err := h.pool.Query(ctx, `
		SELECT review_type, count(*)
		FROM review_item
		WHERE household_id=$1 AND created_at >= now() - interval '30 days'
		GROUP BY review_type`, householdID)
	if err != nil {
		return aggregate, err
	}
	defer reasonRows.Close()
	for reasonRows.Next() {
		var reviewType string
		var count int
		if err := reasonRows.Scan(&reviewType, &count); err != nil {
			return aggregate, err
		}
		aggregate.ReviewByReason[reviewType] = count
	}
	if err := reasonRows.Err(); err != nil {
		return aggregate, err
	}

	if aggregate.SourceEvents > 0 {
		aggregate.HumanTouchRate = float64(aggregate.ReviewedEvents) / float64(aggregate.SourceEvents)
	}
	// Only populated ReviewDecision contracts support the known-fact re-ask
	// metric; historical null decisions are excluded from its denominator.
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE EXISTS (
			SELECT 1 FROM jsonb_array_elements_text(coalesce(ri.decision->'missingFacts','[]'::jsonb)) AS missing(fact)
			WHERE ri.decision->'knownFacts' ? missing.fact
			  AND ri.decision->'knownFacts'->missing.fact <> 'null'::jsonb
		)) FROM review_item ri
		WHERE ri.household_id=$1 AND ri.created_at >= now()-interval '30 days'
		  AND jsonb_typeof(ri.decision->'missingFacts')='array'`, householdID).
		Scan(&aggregate.ReviewsWithDecision, &aggregate.KnownFactReasks); err != nil {
		return aggregate, err
	}
	if aggregate.ReviewsWithDecision > 0 {
		aggregate.KnownFactReaskRate = float64(aggregate.KnownFactReasks) / float64(aggregate.ReviewsWithDecision)
	}
	// Only bounded validation reviews with explicit entry provenance are eligible;
	// an extracted known fact alone is not proof that validation accepted it.
	// Conflicts, canonical ambiguity and human policy are excluded by consequence.
	if err := h.pool.QueryRow(ctx, `
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
		FROM reviews`, householdID).Scan(&aggregate.ValidatorEligibleReviews, &aggregate.ValidatorInducedReviews, &aggregate.ValidatorUnknownReviews); err != nil {
		return aggregate, err
	}
	if aggregate.ValidatorEligibleReviews > 0 {
		aggregate.ValidatorInducedRate = float64(aggregate.ValidatorInducedReviews) / float64(aggregate.ValidatorEligibleReviews)
	}
	// A NULL accepted set is historical/unknown; '{}' is an explicit empty set.
	// Evidence support and same-event identity checks do not make a semantic
	// re-decision, even if they mention a previously accepted fact.
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE accepted_dimensions_at_entry IS NOT NULL),
		  count(*) FILTER (WHERE accepted_dimensions_at_entry IS NOT NULL AND accepted_dimensions_at_entry && answered_dimensions),
		  count(*) FILTER (WHERE accepted_dimensions_at_entry IS NULL)
		FROM intelligence_phase_telemetry
		WHERE household_id=$1 AND created_at>=now()-interval '30 days'
		  AND capability='JEV' AND outcome='SUCCEEDED' AND cardinality(answered_dimensions)>0
		  AND purpose<>'EVIDENCE_SUPPORT'
		  AND NOT (purpose='OTHER_BOUNDED' AND semantic_dimensions<@ARRAY['same_real_event']::text[])`, householdID).
		Scan(&aggregate.SemanticEligiblePhases, &aggregate.SemanticReDecisions, &aggregate.SemanticUnknownPhases); err != nil {
		return aggregate, err
	}
	if aggregate.SemanticEligiblePhases > 0 {
		aggregate.SemanticReDecisionRate = float64(aggregate.SemanticReDecisions) / float64(aggregate.SemanticEligiblePhases)
	}
	// Fidelity is a property of the stored contract, not labelled semantic
	// ground truth. Only complete ReviewDecision contracts enter the denominator;
	// historical/incomplete decisions remain unknown. Changed fields come from
	// the existing cross-surface review-turn telemetry, not raw resolution values.
	if err := h.pool.QueryRow(ctx, `
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
		     OR (resolution_action='SET_PAY_DATE' AND NOT (decision->'missingFacts' ? 'transaction_at'))
		     OR (resolution_action='SET_FINANCIAL_EMAIL_ENTITIES' AND NOT (decision->'missingFacts' ?| ARRAY['funding_account','wealth_account']))
		     OR (review_type<>'MANUAL_CORRECTION' AND COALESCE(resolution_action,'') NOT IN ('EDIT','CORRECT')
		       AND EXISTS (SELECT 1 FROM product_telemetry_event e CROSS JOIN LATERAL unnest(e.changed_fields) f(field)
		         WHERE e.review_item_id=contracts.id AND e.event_type='REVIEW_TURN'
		           AND NOT (decision->'missingFacts' ? CASE f.field WHEN 'amount_idr' THEN 'amount' WHEN 'account' THEN 'funding_account' ELSE f.field END)))
		   ) AS violation FROM contracts
		)
		SELECT count(*) FILTER (WHERE eligible),count(*) FILTER (WHERE violation),
		  count(*) FILTER (WHERE NOT COALESCE(eligible,false)) FROM scored`, householdID).
		Scan(&aggregate.ResidualEligibleReviews, &aggregate.ResidualViolations, &aggregate.ResidualUnknownReviews); err != nil {
		return aggregate, err
	}
	if aggregate.ResidualEligibleReviews > 0 {
		aggregate.ResidualFidelityRate = float64(aggregate.ResidualEligibleReviews-aggregate.ResidualViolations) / float64(aggregate.ResidualEligibleReviews)
	}

	// Every metric below is derived from canonical state plus the append-only
	// review-turn telemetry the writers already emit, so it cannot drift from the
	// ledger. PRD IR-03: RHICE counts each supplied field or bounded choice; turns
	// may contain multiple fields. Values merged from known server state are not
	// counted. Residual allocation is review metadata, not a canonical transaction
	// input, so it is excluded.
	if err := h.pool.QueryRow(ctx, `
		WITH recent_transactions AS (
		  SELECT id FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND created_at >= now() - interval '30 days'
		), stale_transactions AS (
		  SELECT DISTINCT transaction_id FROM review_item
		  WHERE household_id=$1 AND transaction_id IS NOT NULL AND status='RESOLVED'
		    AND resolved_at < now() - interval '30 days'
		), review_events AS (
		  SELECT DISTINCT ri.id,ri.resolution_action
		  FROM review_item ri
	  JOIN recent_transactions t ON ri.transaction_id=t.id
	    OR (ri.resolution_action='MERGE_REVIEW' AND ri.resolution_values->>'transaction_id'=t.id::text)
	    OR EXISTS (
		    SELECT 1 FROM transaction_evidence te
		    WHERE te.transaction_id=t.id AND (
		      (ri.financial_email_observation_id IS NOT NULL AND EXISTS (SELECT 1 FROM financial_email_observation feo WHERE feo.id=ri.financial_email_observation_id AND feo.transaction_id=t.id))
		      OR (ri.wealth_observation_id IS NOT NULL AND te.metadata_json->>'observation_id'=ri.wealth_observation_id::text)
		      OR (ri.financial_email_observation_id IS NULL AND te.source_event_id=COALESCE(
		        ri.source_event_id,
		        (SELECT source_event_id FROM transaction_proposal WHERE id=ri.proposal_id),
		        (SELECT source_event_id FROM document WHERE id=ri.document_id)))
		    )
		  )
		  WHERE ri.household_id=$1 AND ri.status='RESOLVED' AND ri.resolved_at >= now() - interval '30 days'
		    AND (ri.transaction_id IS NULL OR ri.transaction_id NOT IN (SELECT transaction_id FROM stale_transactions))
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
		FROM review_events`, householdID).Scan(&aggregate.ExplicitInputs, &aggregate.TypedFields, &aggregate.OpenReviews, &aggregate.AcceptedWithoutEdit); err != nil {
		return aggregate, err
	}
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM transaction
		WHERE household_id=$1 AND status='CONFIRMED' AND created_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.CanonicalEvents); err != nil {
		return aggregate, err
	}
	if aggregate.CanonicalEvents > 0 {
		aggregate.RHICE = float64(aggregate.ExplicitInputs) / float64(aggregate.CanonicalEvents)
	}
	// Section 22.2: the open-to-resolve interval is the time to canonical state.
	// It is read from review_item, not review_request, so every review type is
	// covered, not only the transaction-bound Telegram requests.
	if err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(avg(EXTRACT(EPOCH FROM (ri.resolved_at - ri.created_at)) * 1000)::bigint, 0)
		FROM review_item ri
		WHERE ri.household_id=$1 AND ri.created_at >= now() - interval '30 days' AND ri.status='RESOLVED'`, householdID).Scan(&aggregate.TimeToResolutionMs); err != nil {
		return aggregate, err
	}
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE event_type='REVIEW_TURN'),COALESCE(sum(bounded_choices) FILTER (WHERE event_type='REVIEW_TURN'),0)
		FROM product_telemetry_event WHERE household_id=$1 AND occurred_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.ReviewRoundTrips, &aggregate.BoundedChoices); err != nil {
		return aggregate, err
	}
	if aggregate.CanonicalEvents > 0 {
		if err := h.pool.QueryRow(ctx, `
			SELECT COALESCE(sum(e.bounded_choices),0)
			FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id
			WHERE e.household_id=$1 AND e.event_type='REVIEW_TURN'
			  AND t.status='CONFIRMED' AND t.created_at >= now()-interval '30 days'`, householdID).Scan(&aggregate.BoundedChoicesPerEvent); err != nil {
			return aggregate, err
		}
		aggregate.BoundedChoicesPerEvent /= float64(aggregate.CanonicalEvents)
	}
	if err := h.pool.QueryRow(ctx, `
		SELECT count(DISTINCT e.transaction_id)
		FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id
		WHERE e.household_id=$1 AND e.event_type='AUTO_CONFIRM_CORRECTION' AND t.auto_confirmed_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.AutoConfirmCorrections); err != nil {
		return aggregate, err
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND auto_confirmed_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.AutoConfirmEvents); err != nil {
		return aggregate, err
	}
	if aggregate.AutoConfirmEvents > 0 {
		aggregate.AutoConfirmCorrectionRate = float64(aggregate.AutoConfirmCorrections) / float64(aggregate.AutoConfirmEvents)
	}
	for _, query := range []struct {
		sql string
		dst map[string]int
	}{
		{`SELECT action,count(*) FROM product_telemetry_event WHERE household_id=$1 AND event_type='CEU_BINDING' AND occurred_at >= now()-interval '30 days' GROUP BY action`, aggregate.CEUBinding},
		{`SELECT field,count(*) FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id CROSS JOIN LATERAL unnest(e.changed_fields) AS changed(field) WHERE e.household_id=$1 AND e.event_type='AUTO_CONFIRM_CORRECTION' AND t.auto_confirmed_at >= now()-interval '30 days' GROUP BY field`, aggregate.AutoConfirmCorrectionFields},
		{`SELECT COALESCE(e.source_type,'unknown'),count(*) FROM product_telemetry_event e JOIN transaction t ON t.id=e.transaction_id WHERE e.household_id=$1 AND e.event_type='AUTO_CONFIRM_CORRECTION' AND t.auto_confirmed_at >= now()-interval '30 days' GROUP BY 1`, aggregate.AutoConfirmCorrectionSource},
	} {
		rows, err := h.pool.Query(ctx, query.sql, householdID)
		if err != nil {
			return aggregate, err
		}
		for rows.Next() {
			var key string
			var count int
			if err := rows.Scan(&key, &count); err != nil {
				rows.Close()
				return aggregate, err
			}
			query.dst[key] = count
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return aggregate, err
		}
		rows.Close()
	}
	// Pre-migration history cannot be reconstructed. Keep that gap visible for
	// the first 30 days after telemetry starts recording real turns/corrections.
	var telemetryHistoryComplete bool
	if err := h.pool.QueryRow(ctx, `SELECT COALESCE(min(occurred_at),now()) <= now()-interval '30 days' FROM product_telemetry_event WHERE household_id=$1`, householdID).Scan(&telemetryHistoryComplete); err != nil {
		return aggregate, err
	}
	aggregate.Coverage = []string{}
	if !telemetryHistoryComplete {
		aggregate.Coverage = append(aggregate.Coverage, "pre_migration_telemetry_history")
	}
	if aggregate.ValidatorUnknownReviews > 0 {
		aggregate.CoverageIncomplete = append(aggregate.CoverageIncomplete, "validator_induced_review_provenance")
	}
	if aggregate.SemanticUnknownPhases > 0 {
		aggregate.CoverageIncomplete = append(aggregate.CoverageIncomplete, "semantic_redecision_phase_provenance")
	}
	if aggregate.ResidualUnknownReviews > 0 {
		aggregate.CoverageIncomplete = append(aggregate.CoverageIncomplete, "residual_contract_history")
	}
	return aggregate, nil
}
