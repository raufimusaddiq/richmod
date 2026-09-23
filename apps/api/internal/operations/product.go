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
	WindowDays     int            `json:"windowDays"`
	Events         int            `json:"canonicalEvents"`
	Confirmed      int            `json:"confirmed"`
	ReviewedEvents int            `json:"reviewedEvents"`
	HumanTouchRate float64        `json:"humanTouchRate"`
	BySource       map[string]int `json:"canonicalEventsBySource"`
	ReviewBySource map[string]int `json:"reviewRateBySource"`
	ReviewByReason map[string]int `json:"reviewRateByReason"`
	Coverage       []string       `json:"notYetMeasurable"`
}

func (h *Handler) loadProductAggregate(ctx context.Context, householdID string) (productAggregate, error) {
	aggregate := productAggregate{WindowDays: 30, BySource: map[string]int{}, ReviewBySource: map[string]int{}, ReviewByReason: map[string]int{}}

	// Canonical financial events: source events that reached a terminal state in
	// the window. PROCESSED/Ignored are terminal; NEEDS_REVIEW still needs input.
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE processing_status='PROCESSED')
		FROM source_event
		WHERE household_id=$1 AND received_at >= now() - interval '30 days'`, householdID).Scan(&aggregate.Events, &aggregate.Confirmed); err != nil {
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

	// Review rate by source: reviews attributed to their originating source type.
	// A review with no source event (legacy/proposal-scoped) is grouped as
	// "unattributed" instead of being dropped, so the total stays honest.
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
		aggregate.ReviewedEvents += count
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

	if aggregate.Events > 0 {
		aggregate.HumanTouchRate = float64(aggregate.ReviewedEvents) / float64(aggregate.Events)
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
