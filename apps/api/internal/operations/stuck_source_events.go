package operations

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StuckSourceEventsSQL finds source events the ledger shows as NEEDS_REVIEW
// that no surface can resolve: none of the active review items (OPEN or
// PENDING_SEND) reachable from the event points back at it, directly or through
// its active-evidence transactions, proposals, document (including album pages), wealth observation or provider-email
// observations. $1 optionally narrows to one household (NULL scans all).
// docs/runbooks/stuck-source-events.md quotes this text; a test keeps them equal.
const StuckSourceEventsSQL = `SELECT se.id, se.household_id, se.source_type, se.received_at,
  EXISTS (SELECT 1 FROM review_item ri WHERE ri.household_id = se.household_id
          AND ri.status IN ('RESOLVED','CANCELLED','EXPIRED') AND ri.source_event_id = se.id) AS had_source_review,
  (SELECT count(*) FROM transaction_evidence te JOIN transaction t ON t.id = te.transaction_id
    WHERE te.source_event_id = se.id AND t.status = 'NEEDS_REVIEW'
      AND COALESCE(te.metadata_json->>'link_status','ACTIVE') = 'ACTIVE') AS needs_review_transactions,
  (SELECT count(*) FROM transaction_proposal p
    WHERE p.source_event_id = se.id AND p.proposal_status = 'NEEDS_REVIEW') AS needs_review_proposals,
  EXISTS (SELECT 1 FROM document d WHERE d.source_event_id = se.id
          OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id)) AS has_document
FROM source_event se
WHERE se.processing_status = 'NEEDS_REVIEW'
  AND se.household_id = COALESCE($1::uuid, se.household_id)
  AND NOT EXISTS (
    SELECT 1 FROM review_item ri
    WHERE ri.household_id = se.household_id AND ri.status IN ('OPEN','PENDING_SEND')
      AND (ri.source_event_id = se.id
        OR ri.transaction_id IN (SELECT te.transaction_id FROM transaction_evidence te WHERE te.source_event_id = se.id
                                 AND COALESCE(te.metadata_json->>'link_status','ACTIVE') = 'ACTIVE')
        OR ri.proposal_id IN (SELECT p.id FROM transaction_proposal p WHERE p.source_event_id = se.id)
        OR ri.document_id IN (SELECT d.id FROM document d WHERE d.source_event_id = se.id
                              OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
        OR ri.wealth_observation_id IN (SELECT w.id FROM wealth_observation w JOIN document d ON d.id = w.document_id
                                        WHERE d.source_event_id = se.id
                                        OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
        OR ri.financial_email_observation_id IN (SELECT f.id FROM financial_email_observation f WHERE f.source_event_id = se.id)))
ORDER BY se.received_at, se.id`

// StuckSourceEvent is one source event with no reachable active review item.
type StuckSourceEvent struct {
	SourceEventID           string    `json:"sourceEventId"`
	HouseholdID             string    `json:"householdId"`
	SourceType              string    `json:"sourceType"`
	ReceivedAt              time.Time `json:"receivedAt"`
	HadSourceReview         bool      `json:"hadSourceReview"`
	NeedsReviewTransactions int       `json:"needsReviewTransactions"`
	NeedsReviewProposals    int       `json:"needsReviewProposals"`
	HasDocument             bool      `json:"hasDocument"`
}

// StuckSourceEvents runs StuckSourceEventsSQL. An empty householdID scans every
// household, which is the operator view; it never mutates anything.
func StuckSourceEvents(ctx context.Context, pool *pgxpool.Pool, householdID string) ([]StuckSourceEvent, error) {
	var scope *string
	if householdID != "" {
		scope = &householdID
	}
	rows, err := pool.Query(ctx, StuckSourceEventsSQL, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []StuckSourceEvent{}
	for rows.Next() {
		var e StuckSourceEvent
		if err := rows.Scan(&e.SourceEventID, &e.HouseholdID, &e.SourceType, &e.ReceivedAt, &e.HadSourceReview, &e.NeedsReviewTransactions, &e.NeedsReviewProposals, &e.HasDocument); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
