package operations

import "context"

// productAggregate is the PRD §22 product scoreboard computed from canonical
// state that already exists: source events, transactions, and review items. It
// is read-only and stores nothing, so it cannot drift from the ledger and needs
// no new pipeline. Amounts and free text are never selected.
//
// Coverage gap, stated honestly rather than faked: explicit user input counts
// (typed fields, bounded choices, round trips) and per-field auto-confirm
// corrections are not reconstructable from current history. This rollup exposes
// a "pending" list naming the signals that will be populated when their stage
// instruments them (PRD §22.1/§22.2/§22.3). Do not present a partial number as
// complete RHICE.
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
	Coverage       []string       `json:"notYetMeasurable"`
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
	aggregate.Coverage = []string{
		"rhice",
		"typed_fields_per_event",
		"bounded_choices_per_event",
		"review_round_trips",
		"review_accepted_without_edit",
		"auto_confirm_correction_rate",
	}
	return aggregate, nil
}
