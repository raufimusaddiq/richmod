package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

type CycleAllocation struct {
	WealthAccountID string
	AmountIDR       string
	Note            string
}

type CycleResidualCommand struct {
	HouseholdID  string
	CaseID       string
	ReviewItemID string
	RequestID    string
	Action       string
	Allocations  []CycleAllocation
	ActorUserID  string
}

type CycleOutcome string

const (
	CycleResolved           CycleOutcome = "RESOLVED"
	CycleStaleRefreshed     CycleOutcome = "STALE_REFRESHED"
	CycleStaleNotApplicable CycleOutcome = "STALE_NOT_APPLICABLE"
)

type CycleResult struct {
	Outcome              CycleOutcome
	Residual             string
	Allocated            string
	CycleStart, CycleEnd string
}

const cycleRefreshOnly = "REFRESH_BASIS"

var (
	ErrCycleCaseNotFound         = errors.New("reviewdomain: cycle residual case not found")
	ErrCycleAllocationsRequired  = errors.New("reviewdomain: at least one allocation is required")
	ErrCycleAllocationInvalid    = errors.New("reviewdomain: allocation must use a positive whole IDR amount and a valid wealth account")
	ErrCycleAllocationDuplicate  = errors.New("reviewdomain: each wealth account may appear once")
	ErrCycleAllocationMismatch   = errors.New("reviewdomain: allocation total must equal the cycle residual")
	ErrCycleWealthAccountInvalid = errors.New("reviewdomain: wealth account must be active and belong to this household")
)

const cycleBasisSQL = `SELECT basis_income_idr::text,basis_expense_idr::text,basis_savings_idr::text,basis_residual_idr::text,
	(SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),
	(SELECT COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),
	(SELECT COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),
	(SELECT (COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)-COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0))::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta'))
	FROM cycle_residual_case c WHERE c.id=$1 AND c.household_id=$2 FOR UPDATE`

func ApplyCycleResidual(ctx context.Context, tx pgx.Tx, cmd CycleResidualCommand) (CycleResult, error) {
	var result CycleResult
	var oldIncome, oldExpense, oldSavings, oldResidual, income, expense, savings, residual string
	err := tx.QueryRow(ctx, cycleBasisSQL, cmd.CaseID, cmd.HouseholdID).Scan(&oldIncome, &oldExpense, &oldSavings, &oldResidual, &income, &expense, &savings, &residual)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrCycleCaseNotFound
	}
	if err != nil {
		return result, err
	}
	result.Residual = residual
	if oldIncome != income || oldExpense != expense || oldSavings != savings || oldResidual != residual {
		recomputed, ok := new(big.Int).SetString(residual, 10)
		if !ok {
			return result, errors.New("reviewdomain: invalid recomputed residual")
		}
		if recomputed.Sign() <= 0 {
			if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action='NO_LONGER_APPLICABLE',resolution_values=jsonb_build_object('recomputed_residual_idr',$3::text),updated_at=now() WHERE id=$1 AND household_id=$4 AND cycle_residual_case_id=$5 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, residual, cmd.HouseholdID, cmd.CaseID); err != nil {
				return result, err
			}
			if err := resolveCycleRequest(ctx, tx, cmd); err != nil {
				return result, err
			}
			result.Outcome = CycleStaleNotApplicable
			return result, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE cycle_residual_case SET basis_income_idr=$2,basis_expense_idr=$3,basis_savings_idr=$4,basis_residual_idr=$5,updated_at=now() WHERE id=$1 AND household_id=$6`, cmd.CaseID, income, expense, savings, residual, cmd.HouseholdID); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(COALESCE(decision,'{}'::jsonb),'{knownFacts,residual_idr}',to_jsonb($2::text),true),updated_at=now() WHERE id=$1 AND household_id=$3 AND cycle_residual_case_id=$4 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, residual, cmd.HouseholdID, cmd.CaseID); err != nil {
			return result, err
		}
		var cycleStart, cycleEnd time.Time
		if err := tx.QueryRow(ctx, `SELECT cycle_start,cycle_end FROM cycle_residual_case WHERE id=$1 AND household_id=$2`, cmd.CaseID, cmd.HouseholdID).Scan(&cycleStart, &cycleEnd); err != nil {
			return result, err
		}
		cardText := CycleResidualCardText(cycleStart, cycleEnd, residual)
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json) SELECT 'EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('review_request_id',r.id::text,'chat_id',rr.telegram_chat_id,'message_id',rr.telegram_message_id,'text',$2::text,'reply_markup',COALESCE((SELECT j.payload_json->'reply_markup' FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.payload_json->>'review_request_id'=r.id::text ORDER BY j.id DESC LIMIT 1),'{"inline_keyboard":[]}'::jsonb)) FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN telegram_identity ti ON ti.telegram_user_id=rr.telegram_chat_id AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active WHERE r.review_item_id=$1 AND r.household_id=$3 AND r.status='OPEN' AND rr.telegram_message_id IS NOT NULL`, cmd.ReviewItemID, cardText, cmd.HouseholdID); err != nil {
			return result, err
		}
		result.Outcome = CycleStaleRefreshed
		return result, nil
	}
	if cmd.Action == cycleRefreshOnly || cmd.Action == "TRANSACTION_MISSING" {
		result.Outcome = CycleStaleRefreshed
		return result, nil
	}
	residualInt, ok := new(big.Int).SetString(residual, 10)
	if !ok {
		return result, errors.New("reviewdomain: invalid residual basis")
	}
	if cmd.Action == "ALLOCATE_RETAINED_BALANCE" {
		total, err := ValidateCycleAllocations(ctx, tx, cmd.HouseholdID, residual, cmd.Allocations)
		if err != nil {
			return result, err
		}
		for _, allocation := range cmd.Allocations {
			if _, err := tx.Exec(ctx, `INSERT INTO cycle_residual_allocation(cycle_residual_case_id,wealth_account_id,amount_idr,note,created_by_user_id) VALUES($1,$2,$3,$4,$5)`, cmd.CaseID, allocation.WealthAccountID, allocation.AmountIDR, cleanText(allocation.Note, 1000), cmd.ActorUserID); err != nil {
				return result, err
			}
		}
		result.Allocated = total.String()
	}
	if residualInt.Sign() <= 0 {
		result.Allocated = "0"
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1 AND household_id=$5 AND cycle_residual_case_id=$6 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, cmd.Action, string(mustCycleValues(cmd)), cmd.HouseholdID, cmd.CaseID); err != nil {
		return result, err
	}
	if err := resolveCycleRequest(ctx, tx, cmd); err != nil {
		return result, err
	}
	result.Outcome = CycleResolved
	return result, nil
}

const (
	CycleReopenTransactionRecorded = "TRANSACTION_RECORDED"
	CycleReopenTransactionVoided   = "TRANSACTION_VOIDED"
)

func RefreshOpenCycleResiduals(ctx context.Context, tx pgx.Tx, householdID string, at time.Time, actorID string) error {
	return RefreshOpenCycleResidualsFor(ctx, tx, householdID, at, actorID, CycleReopenTransactionRecorded)
}

func RefreshOpenCycleResidualsFor(ctx context.Context, tx pgx.Tx, householdID string, at time.Time, actorID, reason string) error {
	rows, err := tx.Query(ctx, `SELECT c.id::text,ri.id::text FROM cycle_residual_case c JOIN review_item ri ON ri.cycle_residual_case_id=c.id WHERE c.household_id=$1 AND ri.status IN ('PENDING_SEND','OPEN') AND $2::timestamptz >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND $2::timestamptz < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta') ORDER BY c.id`, householdID, at)
	if err != nil {
		return err
	}
	type target struct{ caseID, itemID string }
	var targets []target
	for rows.Next() {
		var v target
		if err := rows.Scan(&v.caseID, &v.itemID); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range targets {
		if _, err := ApplyCycleResidual(ctx, tx, CycleResidualCommand{HouseholdID: householdID, CaseID: v.caseID, ReviewItemID: v.itemID, ActorUserID: actorID, Action: cycleRefreshOnly}); err != nil {
			return err
		}
	}
	waiting, err := tx.Query(ctx, `SELECT c.id::text FROM cycle_residual_case c WHERE c.household_id=$1
		AND $2::timestamptz >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND $2::timestamptz < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')
		AND NOT EXISTS (SELECT 1 FROM review_item o WHERE o.cycle_residual_case_id=c.id AND o.status IN ('PENDING_SEND','OPEN'))
		AND (SELECT ri.resolution_action FROM review_item ri WHERE ri.cycle_residual_case_id=c.id ORDER BY ri.resolved_at DESC NULLS LAST,ri.created_at DESC LIMIT 1)='TRANSACTION_MISSING'
		ORDER BY c.id`, householdID, at)
	if err != nil {
		return err
	}
	cases, err := pgx.CollectRows(waiting, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, caseID := range cases {
		if _, err := reopenCycleResidual(ctx, tx, householdID, caseID, actorID, reason); err != nil {
			return err
		}
	}
	return nil
}

func MarkCycleTransactionMissing(ctx context.Context, tx pgx.Tx, cmd CycleResidualCommand) (CycleResult, error) {
	var result CycleResult
	if err := tx.QueryRow(ctx, `SELECT c.basis_residual_idr::text,c.cycle_start::text,c.cycle_end::text FROM cycle_residual_case c JOIN review_item ri ON ri.cycle_residual_case_id=c.id AND ri.household_id=c.household_id
		WHERE c.id=$1 AND c.household_id=$2 AND ri.id=$3 AND ri.status IN ('PENDING_SEND','OPEN') FOR UPDATE OF c,ri`, cmd.CaseID, cmd.HouseholdID, cmd.ReviewItemID).Scan(&result.Residual, &result.CycleStart, &result.CycleEnd); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrCycleCaseNotFound
		}
		return result, err
	}
	values, err := json.Marshal(map[string]string{"action": "TRANSACTION_MISSING", "residual_idr": result.Residual, "next_step": "RECORD_MISSING_TRANSACTION"})
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action='TRANSACTION_MISSING',resolution_values=$3::jsonb,updated_at=now() WHERE id=$1 AND household_id=$4 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, string(values), cmd.HouseholdID); err != nil {
		return result, err
	}
	if err := resolveCycleRequest(ctx, tx, cmd); err != nil {
		return result, err
	}
	result.Outcome = CycleResolved
	return result, nil
}

func reopenCycleResidual(ctx context.Context, tx pgx.Tx, householdID, caseID, actorID, reason string) (string, error) {
	var oldIncome, oldExpense, oldSavings, oldResidual, income, expense, savings, residual string
	if err := tx.QueryRow(ctx, cycleBasisSQL, caseID, householdID).Scan(&oldIncome, &oldExpense, &oldSavings, &oldResidual, &income, &expense, &savings, &residual); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCycleCaseNotFound
		}
		return "", err
	}
	if amount, ok := new(big.Int).SetString(residual, 10); !ok || amount.Sign() <= 0 {
		return "", nil
	}
	if _, err := tx.Exec(ctx, `UPDATE cycle_residual_case SET basis_income_idr=$2,basis_expense_idr=$3,basis_savings_idr=$4,basis_residual_idr=$5,updated_at=now() WHERE id=$1 AND household_id=$6`, caseID, income, expense, savings, residual, householdID); err != nil {
		return "", err
	}
	decision, ok := reviewdec.Preset("CYCLE_RESIDUAL_ALLOCATION", "cycle_residual_case", caseID)
	if !ok {
		return "", errors.New("reviewdomain: no CYCLE_RESIDUAL_ALLOCATION contract")
	}
	decision.KnownFacts["residual_idr"] = residual
	decision.Provenance["reopened"] = reason
	encoded, err := decision.JSON()
	if err != nil {
		return "", err
	}
	var itemID string
	if err := tx.QueryRow(ctx, `INSERT INTO review_item(household_id,review_type,status,cycle_residual_case_id,decision) SELECT $1,'CYCLE_RESIDUAL_ALLOCATION','OPEN',$2,$3::jsonb
		WHERE NOT EXISTS (SELECT 1 FROM review_item WHERE household_id=$1 AND cycle_residual_case_id=$2 AND status IN ('OPEN','PENDING_SEND'))
		ON CONFLICT (cycle_residual_case_id) WHERE cycle_residual_case_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN') DO NOTHING RETURNING id::text`, householdID, caseID, string(encoded)).Scan(&itemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if err := auditReopen(ctx, tx, householdID, actorID, itemID, map[string]any{"cycle_residual_case_id": caseID, "residual_idr": residual, "reason": reason}); err != nil {
		return "", err
	}
	return itemID, EnqueueReviewProjection(ctx, tx, householdID, itemID)
}

func resolveCycleRequest(ctx context.Context, tx pgx.Tx, cmd CycleResidualCommand) error {
	if cmd.RequestID != "" {
		_, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND household_id=$2 AND review_item_id=$3 AND status IN ('PENDING_SEND','OPEN')`, cmd.RequestID, cmd.HouseholdID, cmd.ReviewItemID)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND household_id=$2 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.HouseholdID)
	return err
}

func ValidateCycleAllocations(ctx context.Context, tx pgx.Tx, householdID, residual string, allocations []CycleAllocation) (*big.Int, error) {
	if len(allocations) == 0 {
		return nil, ErrCycleAllocationsRequired
	}
	residualInt, ok := new(big.Int).SetString(residual, 10)
	if !ok {
		return nil, errors.New("reviewdomain: invalid residual basis")
	}
	total := new(big.Int)
	seen := make(map[string]struct{}, len(allocations))
	for _, allocation := range allocations {
		amount, valid := new(big.Int).SetString(allocation.AmountIDR, 10)
		if allocation.WealthAccountID == "" || !valid || amount.Sign() <= 0 {
			return nil, ErrCycleAllocationInvalid
		}
		if _, duplicate := seen[allocation.WealthAccountID]; duplicate {
			return nil, ErrCycleAllocationDuplicate
		}
		seen[allocation.WealthAccountID] = struct{}{}
		total.Add(total, amount)
	}
	if total.Cmp(residualInt) != 0 {
		return nil, ErrCycleAllocationMismatch
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	var validAccounts int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM wealth_account WHERE household_id=$1 AND active AND id=ANY($2::uuid[])`, householdID, ids).Scan(&validAccounts); err != nil {
		return nil, err
	}
	if validAccounts != len(ids) {
		return nil, ErrCycleWealthAccountInvalid
	}
	return total, nil
}

func mustCycleValues(cmd CycleResidualCommand) []byte {
	type allocationValues struct {
		WealthAccountID string `json:"wealthAccountId"`
		AmountIDR       string `json:"amountIdr"`
		Note            string `json:"note"`
	}
	payload := struct {
		Action      string             `json:"action"`
		Allocations []allocationValues `json:"allocations,omitempty"`
	}{Action: cmd.Action}
	for _, allocation := range cmd.Allocations {
		payload.Allocations = append(payload.Allocations, allocationValues(allocation))
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"action":"` + cmd.Action + `"}`)
	}
	return encoded
}
