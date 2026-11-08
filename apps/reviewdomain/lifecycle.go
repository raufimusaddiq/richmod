package reviewdomain

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// RefreshSourceLifecycle settles the whole document, including album members and
// proposal-only reviews. One completed row never settles its unresolved siblings.
//
// It is the only place a source event's processing_status is derived after a
// review transition (ADR-046 amendment, 2026-11). Every lane's evidence state
// counts: transactions and proposals, financial email observations, transfer
// reconciliation cases, wealth observations and open review items. A source a
// resolver dismissed is first marked with MarkSourceIgnored; the refresh keeps
// IGNORED only while nothing on the source was accepted.
//
// A wealth observation counts the same way whatever its origin: an ACCEPTED or
// APPLIED one is accepted evidence and a DISMISSED one is rejected evidence,
// whether it came from a document or a provider email. For a provider email the
// financial email observation's own status usually says the same thing too
// (IGNORED on an Inbox dismissal); the wealth clause no longer depends on that.
func RefreshSourceLifecycle(ctx context.Context, tx pgx.Tx, householdID, sourceID string) error {
	// Serialize sibling completion before taking the aggregate's fresh snapshot.
	if _, err := tx.Exec(ctx, `SELECT s.id FROM source_event s WHERE s.household_id=$2 AND s.id IN (
		SELECT $1::uuid UNION SELECT d.source_event_id FROM document d
		WHERE d.household_id=$2 AND EXISTS(SELECT 1 FROM document_page dp WHERE dp.document_id=d.id AND dp.source_event_id=$1)
	) AND EXISTS(SELECT 1 FROM source_event WHERE id=$1 AND household_id=$2) ORDER BY s.id FOR UPDATE`, sourceID, householdID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `WITH documents AS (
		SELECT d.id,d.source_event_id FROM document d WHERE d.household_id=$2 AND (d.source_event_id=$1
		OR EXISTS(SELECT 1 FROM document_page dp WHERE dp.document_id=d.id AND dp.source_event_id=$1))
		AND EXISTS(SELECT 1 FROM source_event WHERE id=$1 AND household_id=$2)
	), sources AS (
		SELECT s.id FROM source_event s WHERE s.household_id=$2 AND s.id IN (
		SELECT id FROM source_event WHERE id=$1 AND household_id=$2 UNION SELECT source_event_id FROM documents
		UNION SELECT dp.source_event_id FROM document_page dp JOIN documents d ON d.id=dp.document_id
		)
	), evidence AS (
		SELECT te.transaction_id FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id AND t.household_id=$2 WHERE te.source_event_id IN (SELECT id FROM sources)
		AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'
	), state AS (
		SELECT EXISTS(SELECT 1 FROM transaction_proposal p WHERE p.household_id=$2 AND p.source_event_id IN (SELECT id FROM sources) AND p.proposal_status='NEEDS_REVIEW')
		OR EXISTS(SELECT 1 FROM transaction t JOIN evidence e ON e.transaction_id=t.id WHERE t.status='NEEDS_REVIEW')
		OR EXISTS(SELECT 1 FROM financial_email_observation feo WHERE feo.household_id=$2 AND feo.source_event_id IN (SELECT id FROM sources) AND feo.status IN ('PENDING','REVIEW'))
		OR EXISTS(SELECT 1 FROM transfer_reconciliation_case trc WHERE trc.household_id=$2 AND trc.source_event_id IN (SELECT id FROM sources) AND trc.status='OPEN')
		OR EXISTS(SELECT 1 FROM review_item ri WHERE ri.household_id=$2 AND ri.status IN ('OPEN','PENDING_SEND') AND (
			ri.source_event_id IN (SELECT id FROM sources) OR ri.document_id IN (SELECT id FROM documents)
			OR ri.transaction_id IN (SELECT transaction_id FROM evidence)
			OR ri.proposal_id IN (SELECT id FROM transaction_proposal WHERE source_event_id IN (SELECT id FROM sources))
			OR ri.wealth_observation_id IN (SELECT id FROM wealth_observation WHERE document_id IN (SELECT id FROM documents))
			OR ri.financial_email_observation_id IN (SELECT id FROM financial_email_observation WHERE source_event_id IN (SELECT id FROM sources))
		)) AS pending,
		EXISTS(SELECT 1 FROM transaction t JOIN evidence e ON e.transaction_id=t.id WHERE t.status='CONFIRMED')
		OR EXISTS(SELECT 1 FROM transaction_proposal WHERE household_id=$2 AND source_event_id IN (SELECT id FROM sources) AND proposal_status IN ('ACCEPTED','MERGED'))
		OR EXISTS(SELECT 1 FROM financial_email_observation feo WHERE feo.household_id=$2 AND feo.source_event_id IN (SELECT id FROM sources) AND feo.status='APPLIED')
		OR EXISTS(SELECT 1 FROM wealth_observation wo WHERE wo.household_id=$2 AND wo.status IN ('ACCEPTED','APPLIED') AND (wo.document_id IN (SELECT id FROM documents)
			OR wo.financial_email_observation_id IN (SELECT id FROM financial_email_observation WHERE household_id=$2 AND source_event_id IN (SELECT id FROM sources)))) AS accepted,
		EXISTS(SELECT 1 FROM transaction_proposal WHERE household_id=$2 AND source_event_id IN (SELECT id FROM sources) AND proposal_status='REJECTED')
		OR EXISTS(SELECT 1 FROM transaction t JOIN evidence e ON e.transaction_id=t.id WHERE t.status='VOIDED')
		OR EXISTS(SELECT 1 FROM financial_email_observation feo WHERE feo.household_id=$2 AND feo.source_event_id IN (SELECT id FROM sources) AND feo.status='IGNORED')
		OR EXISTS(SELECT 1 FROM wealth_observation wo WHERE wo.household_id=$2 AND wo.status='DISMISSED' AND (wo.document_id IN (SELECT id FROM documents)
			OR wo.financial_email_observation_id IN (SELECT id FROM financial_email_observation WHERE household_id=$2 AND source_event_id IN (SELECT id FROM sources)))) AS rejected
	), settled_documents AS (
		UPDATE document SET status=CASE WHEN state.pending THEN 'NEEDS_REVIEW' ELSE 'EXTRACTED' END,updated_at=now()
		FROM state WHERE household_id=$2 AND id IN (SELECT id FROM documents) AND status IN ('NEEDS_REVIEW','EXTRACTED')
	)
	UPDATE source_event SET processing_status=CASE WHEN state.pending THEN 'NEEDS_REVIEW'
		WHEN state.rejected AND NOT state.accepted THEN 'IGNORED'
		WHEN processing_status='IGNORED' AND NOT state.accepted THEN 'IGNORED'
		WHEN processing_status='FAILED' AND NOT state.accepted THEN 'FAILED' ELSE 'PROCESSED' END
	FROM state WHERE household_id=$2 AND id IN (SELECT id FROM sources)`, sourceID, householdID)
	return err
}

func RefreshTransactionLifecycle(ctx context.Context, tx pgx.Tx, householdID, transactionID string) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT e.source_event_id::text FROM transaction_evidence e JOIN transaction t ON t.id=e.transaction_id WHERE t.id=$1 AND t.household_id=$2 AND COALESCE(e.metadata_json->>'link_status','ACTIVE')='ACTIVE' ORDER BY e.source_event_id::text`, transactionID, householdID)
	if err != nil {
		return err
	}
	sources, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := RefreshSourceLifecycle(ctx, tx, householdID, source); err != nil {
			return err
		}
	}
	return nil
}

// MarkSourceIgnored records that a household dismissed the evidence of one
// source and then derives the settled status through RefreshSourceLifecycle, so
// an accepted sibling (another observation, a linked transaction) still wins and
// an open sibling review keeps the source in NEEDS_REVIEW.
func MarkSourceIgnored(ctx context.Context, tx pgx.Tx, householdID, sourceID string) error {
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED' WHERE id=$1 AND household_id=$2`, sourceID, householdID); err != nil {
		return err
	}
	return RefreshSourceLifecycle(ctx, tx, householdID, sourceID)
}
