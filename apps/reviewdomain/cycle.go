// Cycle residual reconciliation. Web and both Telegram lanes recomputed
// the same cycle basis and applied the same stale/allocation rules inline; this
// operation owns that logic so a change cannot drift between surfaces.
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

// CycleAllocation is one requested retained-balance allocation.
type CycleAllocation struct {
	WealthAccountID string
	AmountIDR       string
	Note            string
}

// CycleResidualCommand resolves one cycle_residual_case review.
//
// Action is the canonical action (ALLOCATE_RETAINED_BALANCE or
// LEAVE_UNALLOCATED); Allocations are required only for allocation. The caller
// must already have authorized the actor and bound the exact review item.
type CycleResidualCommand struct {
	HouseholdID string
	// CaseID is the cycle_residual_case under review; it is locked for update.
	CaseID string
	// ReviewItemID and RequestID pin the exact projection to complete.
	ReviewItemID string
	RequestID    string
	Action       string
	Allocations  []CycleAllocation
	// ActorUserID owns the allocations and the resolution.
	ActorUserID string
}

// CycleOutcome is the canonical result the surfaces map to their own wording.
type CycleOutcome string

const (
	// CycleResolved means the review is complete and any allocations are stored.
	CycleResolved CycleOutcome = "RESOLVED"
	// CycleStaleRefreshed means the recomputed basis differed with a positive
	// residual; the case basis was refreshed and the review stays open.
	CycleStaleRefreshed CycleOutcome = "STALE_REFRESHED"
	// CycleStaleNotApplicable means the recomputed residual is no longer positive;
	// the review was completed as no-longer-applicable without allocations.
	CycleStaleNotApplicable CycleOutcome = "STALE_NOT_APPLICABLE"
)

// CycleResult reports the outcome plus the residual the decision saw.
type CycleResult struct {
	Outcome   CycleOutcome
	Residual  string
	Allocated string
	// CycleStart and CycleEnd are the Jakarta dates of the residual cycle, set by
	// MarkCycleTransactionMissing so a surface can open manual entry there.
	CycleStart, CycleEnd string
}

// cycleRefreshOnly is the internal command RefreshOpenCycleResiduals uses to
// re-evaluate an open review's basis without completing it. It is not a
// household action.
const cycleRefreshOnly = "REFRESH_BASIS"

var (
	// ErrCycleCaseNotFound reports a cycle case outside the household.
	ErrCycleCaseNotFound = errors.New("reviewdomain: cycle residual case not found")
	// ErrCycleAllocationsRequired reports an allocation with no allocation rows.
	ErrCycleAllocationsRequired = errors.New("reviewdomain: at least one allocation is required")
	// ErrCycleAllocationInvalid reports an allocation with a bad account or amount.
	ErrCycleAllocationInvalid = errors.New("reviewdomain: allocation must use a positive whole IDR amount and a valid wealth account")
	// ErrCycleAllocationDuplicate reports the same wealth account twice.
	ErrCycleAllocationDuplicate = errors.New("reviewdomain: each wealth account may appear once")
	// ErrCycleAllocationMismatch reports allocations that do not sum to residual.
	ErrCycleAllocationMismatch = errors.New("reviewdomain: allocation total must equal the cycle residual")
	// ErrCycleWealthAccountInvalid reports an inactive or foreign wealth account.
	ErrCycleWealthAccountInvalid = errors.New("reviewdomain: wealth account must be active and belong to this household")
)

// cycleBasisSQL locks the case and returns the stored basis alongside the basis
// recomputed from confirmed transactions in the cycle window.
const cycleBasisSQL = `SELECT basis_income_idr::text,basis_expense_idr::text,basis_savings_idr::text,basis_residual_idr::text,
	(SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),
	(SELECT COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),
	(SELECT COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),
	(SELECT (COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)-COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0))::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta'))
	FROM cycle_residual_case c WHERE c.id=$1 AND c.household_id=$2 FOR UPDATE`

// ApplyCycleResidual locks the case, revalidates the recomputed basis, applies
// any retained-balance allocations, and completes the review. Stale bases are
// never guessed: a changed positive residual refreshes the stored basis and
// returns CycleStaleRefreshed, and a non-positive residual closes the review as
// no-longer-applicable.
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
			if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='NO_LONGER_APPLICABLE',resolution_values=jsonb_build_object('recomputed_residual_idr',$3::text),updated_at=now() WHERE id=$1 AND household_id=$4 AND cycle_residual_case_id=$5 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, residual, cmd.HouseholdID, cmd.CaseID); err != nil {
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
		// Edit bound cards in the same commit. The sender falls back to a new
		// message if Telegram can no longer edit the original card.
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json) SELECT 'EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('review_request_id',r.id::text,'chat_id',rr.telegram_chat_id,'message_id',rr.telegram_message_id,'text','Sisa salary cycle diperbarui: Rp'||$2::text||'. Balas kartu ini untuk mengalokasikan sisa saldo atau mencatat transaksi yang belum masuk.','reply_markup',COALESCE((SELECT j.payload_json->'reply_markup' FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.payload_json->>'review_request_id'=r.id::text ORDER BY j.id DESC LIMIT 1),'{"inline_keyboard":[]}'::jsonb)) FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN telegram_identity ti ON ti.telegram_user_id=rr.telegram_chat_id AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active WHERE r.review_item_id=$1 AND r.household_id=$3 AND r.status='OPEN' AND rr.telegram_message_id IS NOT NULL`, cmd.ReviewItemID, residual, cmd.HouseholdID); err != nil {
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
		// Defensive: a zero basis with no stale change should not allocate.
		result.Allocated = "0"
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1 AND household_id=$5 AND cycle_residual_case_id=$6 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, cmd.Action, string(mustCycleValues(cmd)), cmd.HouseholdID, cmd.CaseID); err != nil {
		return result, err
	}
	if err := resolveCycleRequest(ctx, tx, cmd); err != nil {
		return result, err
	}
	result.Outcome = CycleResolved
	return result, nil
}

// RefreshOpenCycleResiduals re-evaluates only open cycle reviews affected by a
// newly confirmed transaction. Run inside the transaction's commit boundary.
func RefreshOpenCycleResiduals(ctx context.Context, tx pgx.Tx, householdID string, at time.Time, actorID string) error {
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
	// A cycle the household marked "transaksi belum tercatat" is asked again once
	// a transaction lands in it, with the recomputed residual.
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
		if _, err := reopenCycleResidual(ctx, tx, householdID, caseID, actorID, "TRANSACTION_RECORDED"); err != nil {
			return err
		}
	}
	return nil
}

// MarkCycleTransactionMissing completes a residual review with the household's
// answer "a transaction is not recorded yet". It writes no transaction and no
// allocation (ADR-036: residuals stay review metadata). The next step is manual
// entry; once a transaction lands in the cycle, RefreshOpenCycleResiduals reopens
// the residual review with the recomputed basis.
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

// reopenCycleResidual recomputes one case's basis and, when money is still
// unexplained, opens a fresh residual review with a newly built decision. It
// returns the new review item id, or "" when nothing remains to allocate.
func reopenCycleResidual(ctx context.Context, tx pgx.Tx, householdID, caseID, actorID, reason string) (string, error) {
	var oldIncome, oldExpense, oldSavings, oldResidual, income, expense, savings, residual string
	if err := tx.QueryRow(ctx, cycleBasisSQL, caseID, householdID).Scan(&oldIncome, &oldExpense, &oldSavings, &oldResidual, &income, &expense, &savings, &residual); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCycleCaseNotFound
		}
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE cycle_residual_case SET basis_income_idr=$2,basis_expense_idr=$3,basis_savings_idr=$4,basis_residual_idr=$5,updated_at=now() WHERE id=$1 AND household_id=$6`, caseID, income, expense, savings, residual, householdID); err != nil {
		return "", err
	}
	if amount, ok := new(big.Int).SetString(residual, 10); !ok || amount.Sign() <= 0 {
		return "", nil
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
	if err := tx.QueryRow(ctx, `INSERT INTO review_item(household_id,review_type,status,cycle_residual_case_id,decision) VALUES($1,'CYCLE_RESIDUAL_ALLOCATION','OPEN',$2,$3::jsonb) ON CONFLICT DO NOTHING RETURNING id::text`, householdID, caseID, string(encoded)).Scan(&itemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if err := auditReopen(ctx, tx, householdID, actorID, itemID, map[string]any{"cycle_residual_case_id": caseID, "residual_idr": residual, "reason": reason}); err != nil {
		return "", err
	}
	return itemID, nil
}

// resolveCycleRequest completes the exact Telegram projection (or every open
// projection when no request is pinned, which is the Web behavior).
func resolveCycleRequest(ctx context.Context, tx pgx.Tx, cmd CycleResidualCommand) error {
	if cmd.RequestID != "" {
		_, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND household_id=$2 AND review_item_id=$3 AND status IN ('PENDING_SEND','OPEN')`, cmd.RequestID, cmd.HouseholdID, cmd.ReviewItemID)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND household_id=$2 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.HouseholdID)
	return err
}

// ValidateCycleAllocations checks uniqueness, positivity, and household
// ownership, and returns the summed total. It never mutates state.
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

// mustCycleValues serializes the canonical resolution values stored on the
// review item. It mirrors the allocation payload the surfaces already sent and
// cannot fail for the fixed struct shape.
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
