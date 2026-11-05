package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// ErrReopenNotReversible reports a resolved review whose resolution cannot be
// undone without hard-deleting or silently rewriting a canonical record (ADR-054
// amendment, 2026-11). The household uses the canonical reversal for that record
// instead (void, unmerge, snapshot correction).
var ErrReopenNotReversible = errors.New("reviewdomain: this resolution is not reversible from review")

// possibleDuplicateDecision is the ReviewDecision for a transaction whose
// duplicate relationship is undecided. The contract is the shared
// POSSIBLE_DUPLICATE kind, so a reopened or screenshot-created review cannot
// advertise different actions or render differently from a freshly created one.
func possibleDuplicateDecision(transactionID string, known map[string]any) ([]byte, error) {
	decision, ok := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", transactionID)
	if !ok {
		return nil, errors.New("reviewdomain: no POSSIBLE_DUPLICATE review contract")
	}
	for key, value := range known {
		decision.KnownFacts[key] = value
	}
	return decision.JSON()
}

// ReopenTransactionReview gives a transaction that a reversal returned to
// NEEDS_REVIEW its canonical active review_item again. Resolved items are
// history and stay untouched. Its only caller is Unmerge, and reversing a merge
// is exactly what makes the duplicate relationship undecided again, so the new
// OPEN item is a fresh POSSIBLE_DUPLICATE decision from the kind registry. It
// never copies an old snapshot: every other residual is recomputed from the
// transaction and its evidence as they are now (an expense with no category, a
// date that only fell back to the received time, an unclassified transfer), so
// a fact the household already supplied is not asked again. The previous kind
// is kept as provenance only.
//
// A newly reopened item is projected to Telegram through the shared projection
// path by a PROJECT_TELEGRAM_REVIEW job, so the household is asked again where
// the review started. Idempotent and race-safe: an existing active item is kept
// as is and is not projected twice.
func ReopenTransactionReview(ctx context.Context, tx pgx.Tx, household, transactionID string) error {
	_, err := reopenTransaction(ctx, tx, household, transactionID, "")
	return err
}

// reopenTransaction is the one transaction reopen path (Unmerge and
// ReopenReview). It returns the new item id, or "" when an active item exists.
func reopenTransaction(ctx context.Context, tx pgx.Tx, household, transactionID, actorID string) (string, error) {
	var amount, at, kind, previousType, previousItem, categorySlug string
	var dateFallback bool
	if err := tx.QueryRow(ctx, `SELECT t.amount::text,t.transaction_at::text,t.type,COALESCE(prev.review_type,''),COALESCE(prev.id::text,''),COALESCE(c.slug,''),
		EXISTS(SELECT 1 FROM transaction_proposal p JOIN transaction_evidence te ON p.id=NULLIF(te.metadata_json->>'proposal_id','')::uuid WHERE te.transaction_id=t.id AND p.household_id=t.household_id AND p.metadata_json->>'date_known'='false' AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE')
		FROM transaction t
		LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id AND c.active
		LEFT JOIN LATERAL (
			SELECT ri.id,ri.review_type FROM review_item ri
			WHERE ri.transaction_id=t.id ORDER BY ri.created_at DESC LIMIT 1
		) prev ON true
		WHERE t.id=$1 AND t.household_id=$2 AND t.status='NEEDS_REVIEW'`, transactionID, household).Scan(&amount, &at, &kind, &previousType, &previousItem, &categorySlug, &dateFallback); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrReopenNotReversible
		}
		return "", err
	}
	decision, ok := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", transactionID)
	if !ok {
		return "", errors.New("reviewdomain: no POSSIBLE_DUPLICATE review contract")
	}
	decision.MissingFacts = append(decision.MissingFacts, reopenedResiduals(kind, categorySlug != "", dateFallback)...)
	decision.KnownFacts["amount_idr"] = amount
	decision.KnownFacts["type"] = kind
	if !dateFallback {
		decision.KnownFacts["transaction_at"] = at
	}
	if categorySlug != "" {
		decision.KnownFacts["category"] = categorySlug
	}
	decision.Provenance["reopened"] = "MERGE_REVERSED"
	if previousItem != "" {
		decision.Provenance["previous_review_item_id"] = previousItem
		decision.Provenance["previous_review_type"] = previousType
	}
	encoded, err := decision.JSON()
	if err != nil {
		return "", err
	}
	var itemID string
	err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision,preferred_user_id)
		SELECT t.household_id,t.id,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb,t.created_by_user_id
		FROM transaction t
		WHERE t.id=$1 AND t.household_id=$2 AND t.status='NEEDS_REVIEW'
		ON CONFLICT (transaction_id) WHERE transaction_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN') DO NOTHING
		RETURNING id`, transactionID, household, string(encoded)).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := auditReopen(ctx, tx, household, actorID, itemID, map[string]any{"transaction_id": transactionID, "review_type": "POSSIBLE_DUPLICATE"}); err != nil {
		return "", err
	}
	return itemID, EnqueueReviewProjection(ctx, tx, household, itemID)
}

func auditReopen(ctx context.Context, tx pgx.Tx, household, actorID, itemID string, after map[string]any) error {
	encoded, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,CASE WHEN $2='' THEN 'SYSTEM' ELSE 'USER' END,NULLIF($2,'')::uuid,'REOPEN_REVIEW','review_item',$3,$4::jsonb)`, household, actorID, itemID, string(encoded))
	return err
}

// reopenedResiduals lists the facts besides the duplicate relationship that the
// transaction still lacks, read from canonical state only: an expense without a
// category, a date that is only the received-time fallback, and an incoming or
// outgoing row whose transfer relationship was never classified.
func reopenedResiduals(transactionType string, hasCategory, dateFallback bool) []string {
	var missing []string
	if transactionType == "EXPENSE" && !hasCategory {
		missing = append(missing, "category")
	}
	if dateFallback {
		missing = append(missing, "transaction_at")
	}
	if transactionType == "UNCLASSIFIED" {
		missing = append(missing, "transfer_relationship")
	}
	return missing
}

// EnqueueReviewProjection asks the worker to project one review item to
// Telegram through the shared projection path. Surfaces that cannot render a
// card themselves (the API) use it; the projection is idempotent per item.
func EnqueueReviewProjection(ctx context.Context, tx pgx.Tx, household, itemID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('PROJECT_TELEGRAM_REVIEW',jsonb_build_object('household_id',$1::uuid,'review_item_id',$2::uuid))`, household, itemID)
	return err
}

// ReopenCommand asks to reopen one resolved review item.
type ReopenCommand struct {
	HouseholdID  string
	ActorUserID  string
	ReviewItemID string
}

// ReopenReview reverses a resolved review where that is possible without
// hard-deleting or silently rewriting a canonical record, and opens a new review
// item with a freshly built decision. The resolved item stays as history. It
// returns the new review item id; every reopened item is projected to Telegram
// through the shared PROJECT_TELEGRAM_REVIEW job.
//
// Reversible (the resolution only changed review or observation state):
//   - transaction reviews whose transaction is NEEDS_REVIEW again;
//   - wealth observations ACCEPTED or DISMISSED (an APPLIED value is inside a
//     snapshot; snapshot correction owns that);
//   - financial email observations IGNORED or still under review (an APPLIED
//     observation wrote a transaction or a value; void/unmerge owns that);
//   - proposals REJECTED (screenshot missing amount, payslip ignored);
//   - source- and document-bound reviews IGNORED with no accepted evidence;
//   - residual reviews left unallocated, marked "transaction missing", or
//     closed as no longer applicable (allocation rows are canonical and stay).
func ReopenReview(ctx context.Context, tx pgx.Tx, cmd ReopenCommand) (string, error) {
	var status, kind string
	var decision []byte
	var resolution *string
	var proposal, source, document, transaction, residual, wealth, financial *string
	err := tx.QueryRow(ctx, `SELECT status,review_type,COALESCE(decision,'{}'::jsonb),resolution_action,proposal_id::text,source_event_id::text,document_id::text,transaction_id::text,cycle_residual_case_id::text,wealth_observation_id::text,financial_email_observation_id::text
		FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.ReviewItemID, cmd.HouseholdID).Scan(&status, &kind, &decision, &resolution, &proposal, &source, &document, &transaction, &residual, &wealth, &financial)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrReopenNotReversible
	}
	if err != nil {
		return "", err
	}
	if status != "RESOLVED" {
		return "", ErrReopenNotReversible
	}
	switch {
	case transaction != nil:
		id, err := reopenTransaction(ctx, tx, cmd.HouseholdID, *transaction, cmd.ActorUserID)
		if err == nil && id == "" {
			return "", ErrReopenNotReversible
		}
		return id, err
	case residual != nil:
		if resolution == nil || (*resolution != "LEAVE_UNALLOCATED" && *resolution != "TRANSACTION_MISSING" && *resolution != "NO_LONGER_APPLICABLE") {
			return "", ErrReopenNotReversible
		}
		id, err := reopenCycleResidual(ctx, tx, cmd.HouseholdID, *residual, cmd.ActorUserID, "HOUSEHOLD_REOPEN")
		if err == nil && id == "" {
			return "", ErrReopenNotReversible
		}
		return id, err
	case wealth != nil:
		result, err := tx.Exec(ctx, `UPDATE wealth_observation SET status='PENDING',updated_at=now() WHERE id=$1 AND household_id=$2 AND status IN ('ACCEPTED','DISMISSED')`, *wealth, cmd.HouseholdID)
		if err != nil {
			return "", err
		}
		if result.RowsAffected() != 1 {
			return "", ErrReopenNotReversible
		}
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='REVIEW',updated_at=now() WHERE household_id=$2 AND transaction_id IS NULL AND id=(SELECT financial_email_observation_id FROM wealth_observation WHERE id=$1 AND household_id=$2) AND status IN ('APPLIED','IGNORED')`, *wealth, cmd.HouseholdID); err != nil {
			return "", err
		}
		id, err := insertReopened(ctx, tx, cmd, kind, "wealth_observation_id", *wealth, "wealth_observation", decision)
		if err != nil {
			return "", err
		}
		return id, RefreshWealthObservationSource(ctx, tx, cmd.HouseholdID, *wealth)
	case financial != nil:
		result, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='REVIEW',updated_at=now() WHERE id=$1 AND household_id=$2 AND transaction_id IS NULL AND status IN ('IGNORED','REVIEW')`, *financial, cmd.HouseholdID)
		if err != nil {
			return "", err
		}
		if result.RowsAffected() != 1 {
			return "", ErrReopenNotReversible
		}
		if _, err := tx.Exec(ctx, `UPDATE transfer_reconciliation_case SET status='OPEN',resolved_at=NULL,resolved_by_user_id=NULL,updated_at=now() WHERE financial_email_observation_id=$1 AND household_id=$2 AND status IN ('DISMISSED','RESOLVED')`, *financial, cmd.HouseholdID); err != nil {
			return "", err
		}
		id, err := insertReopened(ctx, tx, cmd, kind, "financial_email_observation_id", *financial, "financial_email_observation", decision)
		if err != nil {
			return "", err
		}
		var sourceID string
		if err := tx.QueryRow(ctx, `SELECT source_event_id::text FROM financial_email_observation WHERE id=$1 AND household_id=$2`, *financial, cmd.HouseholdID).Scan(&sourceID); err != nil {
			return "", err
		}
		return id, RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, sourceID)
	case proposal != nil:
		var sourceID string
		err := tx.QueryRow(ctx, `UPDATE transaction_proposal SET proposal_status='NEEDS_REVIEW',updated_at=now() WHERE id=$1 AND household_id=$2 AND proposal_status='REJECTED' RETURNING source_event_id::text`, *proposal, cmd.HouseholdID).Scan(&sourceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrReopenNotReversible
		}
		if err != nil {
			return "", err
		}
		id, err := insertReopened(ctx, tx, cmd, kind, "proposal_id", *proposal, "proposal", decision)
		if err != nil {
			return "", err
		}
		return id, RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, sourceID)
	case source != nil || document != nil:
		var sourceID string
		if source != nil {
			sourceID = *source
		} else if err := tx.QueryRow(ctx, `SELECT source_event_id::text FROM document WHERE id=$1 AND household_id=$2`, *document, cmd.HouseholdID).Scan(&sourceID); err != nil {
			return "", err
		}
		var reversible bool
		if err := tx.QueryRow(ctx, `SELECT s.processing_status='IGNORED' AND NOT EXISTS(SELECT 1 FROM transaction_evidence te WHERE te.source_event_id=s.id AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE') FROM source_event s WHERE s.id=$1 AND s.household_id=$2`, sourceID, cmd.HouseholdID).Scan(&reversible); err != nil {
			return "", err
		}
		if !reversible {
			return "", ErrReopenNotReversible
		}
		if _, err := tx.Exec(ctx, `UPDATE transfer_reconciliation_case SET status='OPEN',resolved_at=NULL,resolved_by_user_id=NULL,updated_at=now() WHERE source_event_id=$1 AND household_id=$2 AND financial_email_observation_id IS NULL AND status='DISMISSED'`, sourceID, cmd.HouseholdID); err != nil {
			return "", err
		}
		column, subjectID, subjectType := "source_event_id", sourceID, "source_event"
		if source == nil {
			column, subjectID, subjectType = "document_id", *document, "document"
		}
		id, err := insertReopened(ctx, tx, cmd, kind, column, subjectID, subjectType, decision)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW' WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID); err != nil {
			return "", err
		}
		return id, RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, sourceID)
	}
	return "", ErrReopenNotReversible
}

// insertReopened writes the new OPEN item for a non-transaction subject. The
// decision is rebuilt: the kind's preset contract when one exists, otherwise
// (evidence-specific producers such as bank email, screenshot missing amount and
// payslip) the producer's own decision, whose facts come from immutable source
// evidence, with its resolution-time state cleared. Payslip policy is re-applied
// against the household's current salary policy.
func insertReopened(ctx context.Context, tx pgx.Tx, cmd ReopenCommand, kind, column, subjectID, subjectType string, previous []byte) (string, error) {
	var rebuilt reviewdec.Decision
	if err := json.Unmarshal(previous, &rebuilt); err != nil || rebuilt.ReasonCode == "" {
		preset, ok := reviewdec.Preset(kind, subjectType, subjectID)
		if !ok {
			return "", ErrReopenNotReversible
		}
		rebuilt = preset
	} else if preset, ok := reviewdec.Preset(kind, subjectType, subjectID); ok && len(rebuilt.KnownFacts) == 0 && len(rebuilt.ProposedFacts) == 0 {
		rebuilt = preset
	}
	if rebuilt.Provenance == nil {
		rebuilt.Provenance = map[string]any{}
	}
	rebuilt.Provenance["reopened"] = true
	if kind == "PAYSLIP_CONFIRMATION" || kind == "MISSING_PAY_DATE" {
		var hasPrimary bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)`, cmd.HouseholdID).Scan(&hasPrimary); err != nil {
			return "", err
		}
		rebuilt = reviewdec.PayslipPolicy(rebuilt, kind, hasPrimary)
	}
	if kind == "WEALTH_OBSERVATION_CONFIRMATION" {
		rebuilt.MissingFacts = []string{"wealth_snapshot_confirmation"}
	}
	encoded, err := rebuilt.JSON()
	if err != nil {
		return "", err
	}
	var itemID string
	// column is one of a fixed set of subject columns chosen above, never input.
	// A subject that already has active work is never given a second open
	// review (a repeated or racing reopen); the partial unique indexes back this.
	err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,`+column+`,review_type,status,decision) SELECT $1,$2,$3,'OPEN',$4::jsonb
		WHERE NOT EXISTS (SELECT 1 FROM review_item WHERE household_id=$1 AND `+column+`=$2 AND status IN ('OPEN','PENDING_SEND')) ON CONFLICT DO NOTHING RETURNING id::text`, cmd.HouseholdID, subjectID, kind, string(encoded)).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrReopenNotReversible
	}
	if err != nil {
		return "", err
	}
	if err := auditReopen(ctx, tx, cmd.HouseholdID, cmd.ActorUserID, itemID, map[string]any{"reopened_from": cmd.ReviewItemID, "review_type": kind, "subject": subjectType}); err != nil {
		return "", err
	}
	// The reopened review is asked again where it started (Telegram projection).
	return itemID, EnqueueReviewProjection(ctx, tx, cmd.HouseholdID, itemID)
}
