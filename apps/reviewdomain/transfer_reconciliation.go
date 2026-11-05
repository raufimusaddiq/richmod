package reviewdomain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrTransferCaseUnavailable = errors.New("reviewdomain: transfer reconciliation unavailable")

type TransferReconciliationCommand struct {
	HouseholdID, ActorUserID, ReviewItemID, CaseID, Action, CandidateID string
	// Purpose and WealthAccountID carry the household's bounded purpose choice
	// for a case that has no proposed purpose (CONFIRM_NEW_TRANSFER). They are
	// ignored when the case already proposes one.
	Purpose, WealthAccountID string
	// ActorType is the canonical audit surface ('USER' for Web, 'TELEGRAM' for
	// the chat lanes). Empty defaults to USER so a surface that forgets it is
	// recorded as a human resolution rather than an unattributed one.
	ActorType string
}

// ReconcileTransfer owns the financial mutation and canonical review lifecycle.
// Callers authenticate the actor and bind the interaction before entering this transaction.
func ReconcileTransfer(ctx context.Context, tx pgx.Tx, cmd TransferReconciliationCommand) (string, error) {
	var sourceID, observationID, accountID, amount, description, purpose, wealthID string
	var at time.Time
	var candidates []string
	err := tx.QueryRow(ctx, `SELECT trc.source_event_id::text,COALESCE(trc.financial_email_observation_id::text,''),trc.account_id::text,trc.amount_idr::text,COALESCE(trc.description,''),trc.proposed_purpose,COALESCE(trc.proposed_wealth_account_id::text,''),trc.transaction_at,trc.candidate_transaction_ids
		FROM review_item ri JOIN transfer_reconciliation_case trc ON trc.household_id=ri.household_id AND ((ri.financial_email_observation_id IS NOT NULL AND trc.financial_email_observation_id=ri.financial_email_observation_id) OR (ri.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id))
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='TRANSFER_CLASSIFICATION' AND ($3='' OR trc.id::text=$3) AND trc.status='OPEN'
		FOR UPDATE OF ri,trc`, cmd.ReviewItemID, cmd.HouseholdID, cmd.CaseID).Scan(&sourceID, &observationID, &accountID, &amount, &description, &purpose, &wealthID, &at, &candidates)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTransferCaseUnavailable
	}
	if err != nil {
		return "", err
	}
	if cmd.Action != "MERGE_EXISTING" && cmd.Action != "CONFIRM_NEW_TRANSFER" && cmd.Action != "IGNORE" {
		return "", ErrTransferCaseUnavailable
	}
	var sourceType string
	if err := tx.QueryRow(ctx, `SELECT source_type FROM source_event WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID).Scan(&sourceType); err != nil {
		return "", ErrTransferCaseUnavailable
	}
	var id string
	if cmd.Action != "IGNORE" {
		// A case with more candidates than a person can compare is narrowed to
		// the best-ranked bounded set; only that set may be merged into, so a
		// surface can never merge into a candidate it did not show. Creating a new
		// transfer beside an overflowing candidate set still fails closed (ADR-036).
		if cmd.Action == "CONFIRM_NEW_TRANSFER" && observationID != "" && len(candidates) > TransferCandidateLimit {
			return "", ErrTransferCaseUnavailable
		}
		ranked, err := RankTransferCandidates(ctx, tx, cmd.HouseholdID, candidates, at)
		if err != nil {
			return "", err
		}
		candidates = ranked
		if purpose == "" {
			// A case without a proposed purpose takes the household's bounded
			// choice; an unknown purpose is refused by the compatibility check.
			purpose, wealthID = strings.TrimSpace(cmd.Purpose), strings.TrimSpace(cmd.WealthAccountID)
			if !validTransferPurpose(purpose) {
				return "", ErrTransferCaseUnavailable
			}
			if _, err := tx.Exec(ctx, `UPDATE transfer_reconciliation_case SET proposed_purpose=$2,proposed_wealth_account_id=NULLIF($3,'')::uuid,updated_at=now() WHERE source_event_id=$1 AND household_id=$4 AND status='OPEN' AND ($5='' OR financial_email_observation_id::text=$5)`, sourceID, purpose, wealthID, cmd.HouseholdID, observationID); err != nil {
				return "", err
			}
		}
		var compatible bool
		if err := tx.QueryRow(ctx, `SELECT transfer_wealth_compatible($1,NULLIF($2,'')::uuid,$3)`, purpose, wealthID, cmd.HouseholdID).Scan(&compatible); err != nil || !compatible {
			return "", ErrTransferCaseUnavailable
		}
		if cmd.Action == "MERGE_EXISTING" {
			var targetAccount, kind, status, targetAmount string
			if cmd.CandidateID == "" {
				return "", ErrTransferCaseUnavailable
			}
			if err := tx.QueryRow(ctx, `SELECT account_id::text,type,status,amount::text FROM transaction WHERE id=$1 AND household_id=$2 AND id=ANY($3::uuid[]) FOR UPDATE`, cmd.CandidateID, cmd.HouseholdID, candidates).Scan(&targetAccount, &kind, &status, &targetAmount); err != nil || targetAccount != accountID || targetAmount != amount || (kind != "TRANSFER" && kind != "UNCLASSIFIED") || status == "VOIDED" {
				return "", ErrTransferCaseUnavailable
			}
			if _, err := tx.Exec(ctx, `UPDATE transaction SET type='TRANSFER',status='CONFIRMED',category_id=NULL,purpose=$2,related_wealth_account_id=NULLIF($3,'')::uuid,description=COALESCE(NULLIF(description,''),NULLIF($4,'')),confirmed_at=COALESCE(confirmed_at,now()),updated_at=now() WHERE id=$1`, cmd.CandidateID, purpose, wealthID, description); err != nil {
				return "", err
			}
			id = cmd.CandidateID
			if kind == "UNCLASSIFIED" || status == "NEEDS_REVIEW" {
				if err := FinalizeTransferReviewLifecycle(ctx, tx, cmd.HouseholdID, cmd.ActorUserID, id, "TRANSFER", "ACCEPTED", nil, "TRANSFER_RECONCILED", "TRANSFER_RECONCILED"); err != nil {
					return "", err
				}
			}
		} else if err := tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,created_by_user_id,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,NULLIF($5,''),$6,$7,NULLIF($8,'')::uuid,now()) RETURNING id`, cmd.HouseholdID, accountID, amount, at, description, cmd.ActorUserID, purpose, wealthID).Scan(&id); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,$3,1) ON CONFLICT DO NOTHING`, id, sourceID, sourceType); err != nil {
			return "", err
		}
		if observationID != "" {
			if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET transaction_id=$2,status='APPLIED',updated_at=now() WHERE id=$1 AND household_id=$3`, observationID, id, cmd.HouseholdID); err != nil {
				return "", err
			}
		}
	}
	caseStatus, resolution, processing := "RESOLVED", cmd.Action, "PROCESSED"
	if cmd.Action == "IGNORE" {
		// A dismissed transfer keeps its historical IGNORED marker; the Inbox and
		// the agent lane both read it as "parked, not reconciled".
		caseStatus, resolution, processing = "DISMISSED", "IGNORED", "IGNORED"
		if observationID != "" {
			if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status='REVIEW'`, observationID, cmd.HouseholdID); err != nil {
				return "", err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET parser_name=CASE WHEN source_type='FINANCIAL_EMAIL' THEN 'financial-email-reconciliation' ELSE 'telegram-transfer' END,parser_version='1' WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE transfer_reconciliation_case SET status=$2,resolved_at=now(),resolved_by_user_id=$3,updated_at=now() WHERE source_event_id=$1 AND household_id=$4 AND ($5='' OR id::text=$5) AND ($6='' OR financial_email_observation_id::text=$6)`, sourceID, caseStatus, cmd.ActorUserID, cmd.HouseholdID, cmd.CaseID, observationID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,updated_at=now() WHERE id=$1 AND household_id=$4`, cmd.ReviewItemID, cmd.ActorUserID, resolution, cmd.HouseholdID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return "", err
	}
	// The surface is recorded on the canonical review item so Admin
	// counts a resolution by the channel that performed it. Per-surface audit rows
	// key off a transaction or source event, which cannot be joined back here.
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action',$5::text))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID, cmd.Action); err != nil {
		return id, err
	}
	// ADR-046: one lifecycle rule settles the source after the case and the
	// review are closed; a dismissal is recorded first so it can stay IGNORED.
	if processing == "IGNORED" {
		return id, MarkSourceIgnored(ctx, tx, cmd.HouseholdID, sourceID)
	}
	return id, RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, sourceID)
}

// TransferCandidateLimit bounds how many reconciliation candidates any surface
// shows and accepts.
const TransferCandidateLimit = 10

// TransferPurposes is the bounded purpose vocabulary a household may choose for
// a transfer whose case proposes none. It mirrors transfer_wealth_compatible.
var TransferPurposes = []string{"INTERNAL_TRANSFER", "SAVINGS_TRANSFER", "INVESTMENT_CONTRIBUTION", "ASSET_PURCHASE", "DEBT_PRINCIPAL_PAYMENT"}

func validTransferPurpose(purpose string) bool {
	for _, value := range TransferPurposes {
		if value == purpose {
			return true
		}
	}
	return false
}

type rowQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// RankTransferCandidates returns the best-ranked bounded candidate set: active
// household transactions from the case's candidate list, closest in time first,
// at most TransferCandidateLimit. Web, Telegram and the resolver use the same
// ranking, so the set a person chooses from is the set the resolver accepts.
func RankTransferCandidates(ctx context.Context, q rowQueryer, householdID string, candidates []string, at time.Time) ([]string, error) {
	if len(candidates) == 0 {
		return []string{}, nil
	}
	rows, err := q.Query(ctx, `SELECT id::text FROM transaction WHERE household_id=$1 AND id=ANY($2::uuid[]) AND status<>'VOIDED' ORDER BY abs(extract(epoch FROM (transaction_at-$3::timestamptz))),id LIMIT $4`, householdID, candidates, at, TransferCandidateLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
