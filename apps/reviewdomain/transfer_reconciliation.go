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
	Purpose, WealthAccountID                                            string
	ActorType                                                           string
}

func ReconcileTransfer(ctx context.Context, tx pgx.Tx, cmd TransferReconciliationCommand) (string, error) {
	var caseID, sourceID, observationID, accountID, amount, description, purpose, wealthID string
	var at time.Time
	var candidates []string
	err := tx.QueryRow(ctx, `SELECT trc.id::text,trc.source_event_id::text,COALESCE(trc.financial_email_observation_id::text,''),trc.account_id::text,trc.amount_idr::text,COALESCE(trc.description,''),trc.proposed_purpose,COALESCE(trc.proposed_wealth_account_id::text,''),trc.transaction_at,trc.candidate_transaction_ids
		FROM review_item ri JOIN transfer_reconciliation_case trc ON trc.household_id=ri.household_id AND ((ri.financial_email_observation_id IS NOT NULL AND trc.financial_email_observation_id=ri.financial_email_observation_id) OR (ri.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id))
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='TRANSFER_CLASSIFICATION' AND ($3='' OR trc.id::text=$3) AND trc.status='OPEN'
		FOR UPDATE OF ri,trc`, cmd.ReviewItemID, cmd.HouseholdID, cmd.CaseID).Scan(&caseID, &sourceID, &observationID, &accountID, &amount, &description, &purpose, &wealthID, &at, &candidates)
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
		if cmd.Action == "CONFIRM_NEW_TRANSFER" && observationID != "" && len(candidates) > TransferCandidateLimit {
			return "", ErrTransferCaseUnavailable
		}
		var target TransferMergeCandidate
		if cmd.Action == "MERGE_EXISTING" {
			if cmd.CandidateID == "" {
				return "", ErrTransferCaseUnavailable
			}
			var locked string
			if err := tx.QueryRow(ctx, `SELECT id::text FROM transaction WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.CandidateID, cmd.HouseholdID).Scan(&locked); err != nil {
				return "", ErrTransferCaseUnavailable
			}
			set, err := LoadTransferMergeSet(ctx, tx, cmd.HouseholdID, caseID)
			if err != nil {
				return "", err
			}
			found := false
			for _, candidate := range set.Candidates {
				if candidate.ID == cmd.CandidateID {
					target, found = candidate, true
				}
			}
			if !found || !target.Mergeable {
				return "", ErrTransferCaseUnavailable
			}
			if purpose == "" {
				purpose, wealthID = target.Purpose, target.WealthAccountID
			}
		}
		if purpose == "" {
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
			kind, status := target.Type, target.Status
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
		if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, at, cmd.ActorUserID); err != nil {
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
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action',$5::text))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID, cmd.Action); err != nil {
		return id, err
	}
	if processing == "IGNORED" {
		return id, MarkSourceIgnored(ctx, tx, cmd.HouseholdID, sourceID)
	}
	return id, RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, sourceID)
}

const TransferCandidateLimit = 10

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

type TransferMergeCandidate struct {
	ID, Type, Status, Amount, Purpose, WealthAccountID string
	TransactionAt                                      time.Time
	Description                                        *string
	SameAccount, SameAmount                            bool
	Compatible                                         bool
	Mergeable                                          bool
}

type TransferMergeSet struct {
	Purpose    string
	Total      int
	Candidates []TransferMergeCandidate
}

func (s TransferMergeSet) Mergeable() int {
	count := 0
	for _, candidate := range s.Candidates {
		if candidate.Mergeable {
			count++
		}
	}
	return count
}

func TransferMergeable(casePurpose string, c TransferMergeCandidate) bool {
	if c.Status == "VOIDED" || !c.SameAccount || !c.SameAmount || !c.Compatible {
		return false
	}
	if casePurpose == "" {
		return c.Type == "TRANSFER" && validTransferPurpose(c.Purpose)
	}
	return c.Type == "TRANSFER" || c.Type == "UNCLASSIFIED"
}

func LoadTransferMergeSet(ctx context.Context, q rowQueryer, householdID, caseID string) (TransferMergeSet, error) {
	var set TransferMergeSet
	rows, err := q.Query(ctx, `SELECT trc.proposed_purpose,cardinality(trc.candidate_transaction_ids),COALESCE(t.id::text,''),COALESCE(t.type,''),COALESCE(t.status,''),COALESCE(t.amount::text,''),COALESCE(t.purpose,''),COALESCE(t.related_wealth_account_id::text,''),COALESCE(t.transaction_at,trc.transaction_at),t.description,
		COALESCE(t.account_id=trc.account_id,false),COALESCE(t.amount=trc.amount_idr,false),
		COALESCE(CASE WHEN trc.proposed_purpose='' THEN transfer_wealth_compatible(t.purpose,t.related_wealth_account_id,trc.household_id) ELSE transfer_wealth_compatible(trc.proposed_purpose,trc.proposed_wealth_account_id,trc.household_id) END,false)
		FROM transfer_reconciliation_case trc
		LEFT JOIN LATERAL (SELECT c.* FROM transaction c WHERE c.id=ANY(trc.candidate_transaction_ids) AND c.household_id=trc.household_id AND c.status<>'VOIDED'
			ORDER BY abs(extract(epoch FROM (c.transaction_at-trc.transaction_at))),c.id LIMIT $3) t ON true
		WHERE trc.id=$1 AND trc.household_id=$2 AND trc.status='OPEN'
		ORDER BY abs(extract(epoch FROM (t.transaction_at-trc.transaction_at))),t.id`, caseID, householdID, TransferCandidateLimit)
	if err != nil {
		return set, err
	}
	defer rows.Close()
	for rows.Next() {
		var c TransferMergeCandidate
		if err := rows.Scan(&set.Purpose, &set.Total, &c.ID, &c.Type, &c.Status, &c.Amount, &c.Purpose, &c.WealthAccountID, &c.TransactionAt, &c.Description, &c.SameAccount, &c.SameAmount, &c.Compatible); err != nil {
			return set, err
		}
		if c.ID == "" {
			continue
		}
		c.Mergeable = TransferMergeable(set.Purpose, c)
		set.Candidates = append(set.Candidates, c)
	}
	return set, rows.Err()
}
