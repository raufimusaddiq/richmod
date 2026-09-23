package operations

import "context"

// productAggregate is the PRD §22 product scoreboard computed from canonical
// state that already exists: source events, transactions, and review items. It
// is read-only and stores nothing, so it cannot drift from the ledger and needs
// no new pipeline. Amounts and free text are never selected.
//
// Every metric below is derived from canonical state, so the aggregate cannot
// disagree with the ledger and needs no write-path instrumentation. Typed-field
// and bounded-choice counts are classified by resolution_action: an action that
// names a typed value is typing, an accept/merge is a bounded choice.
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

	// RHICE is the PRD 2.2 north-star metric: explicit user inputs required before
	// each canonical financial event reached a valid canonical state, divided by
	// the number of canonical financial events. A resolution row is one explicit
	// input (a button, a dropdown, or a typed value); a canonical event is one
	// transaction. Derived, not written, so it cannot drift from the ledger.
	CanonicalEvents int     `json:"canonicalEvents"`
	ExplicitInputs  int     `json:"explicitInputs"`
	RHICE           float64 `json:"rhice"`
	TypedFields     int     `json:"typedFields"`
	// Reviews still open is the friction the proposal-first card targets.
	OpenReviews            int `json:"openReviews"`
	AcceptedWithoutEdit    int `json:"reviewAcceptedWithoutEdit"`
	AutoConfirmCorrections int `json:"autoConfirmCorrections"`
}

func (h *Handler) loadProductAggregate(ctx context.Context, householdID string) (productAggregate, error) {
	aggregate := productAggregate{WindowDays: 30, BySource: map[string]int{}, ReviewBySource: map[string]int{}, ReviewByReason: map[string]int{}}

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

	// PRD sections 22.1-22.3 derived from state that already exists. Each resolved
	// review_item row is one explicit input; a resolution whose action names a
	// typed value (not a bounded accept/merge/ignore) also counts toward typed
	// fields, the expensive half section 2.3 measures. A canonical event is one
	// transaction, so RHICE is explicit inputs over transactions.
	if err := h.pool.QueryRow(ctx, `
		SELECT
		 count(*) FILTER (WHERE ri.status='RESOLVED'),
		 count(*) FILTER (WHERE ri.status='RESOLVED' AND ri.resolution_action IN ('COMPLETE_BANK_FACTS','SET_PAY_DATE','SET_FINANCIAL_EMAIL_ENTITIES')),
		 count(*) FILTER (WHERE ri.status='OPEN'),
		 count(*) FILTER (WHERE ri.status='RESOLVED' AND ri.resolution_action IN ('CONFIRM_PROPOSAL','MERGE_EXISTING','ACCEPT_PROPOSAL'))
		FROM review_item ri
		WHERE ri.household_id=$1 AND ri.created_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.ExplicitInputs, &aggregate.TypedFields, &aggregate.OpenReviews, &aggregate.AcceptedWithoutEdit); err != nil {
		return aggregate, err
	}
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM transaction
		WHERE household_id=$1 AND created_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.CanonicalEvents); err != nil {
		return aggregate, err
	}
	// Section 22.3 guardrail: a resolution value naming a field the ledger already
	// held is a material correction after auto-confirm.
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM review_item ri
		WHERE ri.household_id=$1 AND ri.created_at >= now() - interval '30 days'
		 AND ri.resolution_values ?| array['amount_idr','transaction_at','category_id','transaction_type']`, householdID).Scan(&aggregate.AutoConfirmCorrections); err != nil {
		return aggregate, err
	}
	if aggregate.CanonicalEvents > 0 {
		aggregate.RHICE = float64(aggregate.ExplicitInputs) / float64(aggregate.CanonicalEvents)
	}
	return aggregate, nil
}
