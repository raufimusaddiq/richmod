package wealth

import (
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialmath"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type optionalString struct {
	Set   bool
	Value *string
}

func (o *optionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

type accountCreateInput struct {
	Name            string  `json:"name"`
	Institution     *string `json:"institution"`
	Side            string  `json:"side"`
	WealthType      string  `json:"wealthType"`
	UsageRole       string  `json:"usageRole"`
	OwnerUserID     *string `json:"ownerUserId"`
	LinkedAccountID *string `json:"linkedAccountId"`
}

type accountPatchInput struct {
	Name            *string        `json:"name"`
	Institution     optionalString `json:"institution"`
	Side            *string        `json:"side"`
	WealthType      *string        `json:"wealthType"`
	UsageRole       *string        `json:"usageRole"`
	OwnerUserID     optionalString `json:"ownerUserId"`
	LinkedAccountID optionalString `json:"linkedAccountId"`
	Active          *bool          `json:"active"`
}

type itemInput struct {
	WealthAccountID string  `json:"wealthAccountId"`
	ValueIDR        string  `json:"valueIdr"`
	Quantity        *string `json:"quantity"`
	Unit            *string `json:"unit"`
	UnitPriceIDR    *string `json:"unitPriceIdr"`
	Source          string  `json:"source"`
	Note            *string `json:"note"`
}

type snapshotInput struct {
	ObservedAt    string      `json:"observedAt"`
	ObservationID *string     `json:"observationId"`
	Items         []itemInput `json:"items"`
}

func (h *Handler) Accounts(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		h.createAccount(w, r, p)
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,name,institution,side,wealth_type,usage_role,owner_user_id,linked_account_id,active,created_at,updated_at FROM wealth_account WHERE household_id=$1 ORDER BY active DESC,name,id`, p.HouseholdID)
	if err != nil {
		fail(w, 500, "unable to list wealth accounts")
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, side, typ, role string
		var institution, ownerID, linkedID *string
		var active bool
		var created, updated time.Time
		if err := rows.Scan(&id, &name, &institution, &side, &typ, &role, &ownerID, &linkedID, &active, &created, &updated); err != nil {
			fail(w, 500, "unable to list wealth accounts")
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "institution": institution, "side": side, "wealthType": typ, "usageRole": role, "ownerUserId": ownerID, "linkedAccountId": linkedID, "active": active, "createdAt": created, "updatedAt": updated})
	}
	if rows.Err() != nil {
		fail(w, 500, "unable to list wealth accounts")
		return
	}
	jsonOut(w, 200, out)
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !owner(w, p) {
		return
	}
	var in accountCreateInput
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !validSide(in.Side, in.WealthType, in.UsageRole) {
		fail(w, 400, "invalid wealth account request")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		fail(w, 500, "unable to create wealth account")
		return
	}
	defer tx.Rollback(r.Context())
	if !validRefs(r, tx, p.HouseholdID, in.OwnerUserID, in.LinkedAccountID) {
		fail(w, 400, "invalid household owner or linked account")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role,owner_user_id,linked_account_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, p.HouseholdID, strings.TrimSpace(in.Name), trim(in.Institution), in.Side, in.WealthType, in.UsageRole, in.OwnerUserID, in.LinkedAccountID).Scan(&id)
	if err != nil {
		writeDBError(w, err, "unable to create wealth account")
		return
	}
	if !audit(r, tx, p, "WEALTH_ACCOUNT_CREATE", "wealth_account", id, nil, in) || tx.Commit(r.Context()) != nil {
		fail(w, 500, "unable to audit wealth account")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}

func (h *Handler) PatchAccount(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok || !owner(w, p) {
		return
	}
	var in accountPatchInput
	if decode(r, &in) != nil || !validPatch(in) {
		fail(w, 400, "invalid wealth account update")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		fail(w, 500, "unable to update wealth account")
		return
	}
	defer tx.Rollback(r.Context())
	var id, name, side, typ, role string
	var institution, ownerID, linkedID *string
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT id,name,institution,side,wealth_type,usage_role,owner_user_id,linked_account_id,active FROM wealth_account WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), p.HouseholdID).Scan(&id, &name, &institution, &side, &typ, &role, &ownerID, &linkedID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "wealth account not found")
		return
	}
	if err != nil {
		fail(w, 500, "unable to update wealth account")
		return
	}
	before := map[string]any{"name": name, "institution": institution, "side": side, "wealthType": typ, "usageRole": role, "ownerUserId": ownerID, "linkedAccountId": linkedID, "active": active}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.UsageRole != nil {
		role = *in.UsageRole
	}
	if in.Institution.Set {
		institution = trim(in.Institution.Value)
	}
	if in.OwnerUserID.Set {
		ownerID = in.OwnerUserID.Value
	}
	if in.LinkedAccountID.Set {
		linkedID = in.LinkedAccountID.Value
	}
	if in.Active != nil {
		active = *in.Active
	}
	if !validSide(side, typ, role) || !validRefs(r, tx, p.HouseholdID, ownerID, linkedID) {
		fail(w, 400, "invalid household owner or linked account")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE wealth_account SET name=$3,institution=$4,usage_role=$5,owner_user_id=$6,linked_account_id=$7,active=$8,updated_at=now() WHERE id=$1 AND household_id=$2`, id, p.HouseholdID, name, institution, role, ownerID, linkedID, active)
	if err != nil {
		writeDBError(w, err, "unable to update wealth account")
		return
	}
	after := map[string]any{"name": name, "institution": institution, "side": side, "wealthType": typ, "usageRole": role, "ownerUserId": ownerID, "linkedAccountId": linkedID, "active": active}
	if !audit(r, tx, p, "WEALTH_ACCOUNT_UPDATE", "wealth_account", id, before, after) || tx.Commit(r.Context()) != nil {
		fail(w, 500, "unable to audit wealth account")
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) Snapshots(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		h.createSnapshot(w, r, p)
		return
	}
	h.listSnapshots(w, r, p.HouseholdID, false)
}

func (h *Handler) createSnapshot(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in snapshotInput
	if decode(r, &in) != nil || !validSnapshot(in) {
		fail(w, 400, "invalid wealth snapshot request")
		return
	}
	at, err := time.Parse(time.RFC3339, in.ObservedAt)
	if err != nil {
		fail(w, 400, "observedAt must be RFC3339")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		fail(w, 500, "unable to create snapshot")
		return
	}
	defer tx.Rollback(r.Context())
	if !completeActiveSet(r, tx, p.HouseholdID, in.Items) {
		fail(w, 400, "snapshot must contain exactly every active wealth account")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO wealth_snapshot(household_id,observed_at,created_by_user_id) VALUES($1,$2,$3) RETURNING id`, p.HouseholdID, at, p.UserID).Scan(&id)
	if err != nil {
		writeDBError(w, err, "unable to create snapshot")
		return
	}
	if !insertItems(r, tx, id, in.Items) {
		fail(w, 400, "unable to create snapshot items")
		return
	}
	if in.ObservationID != nil && !applyObservation(r, tx, p.HouseholdID, *in.ObservationID, in.Items) {
		fail(w, 400, "wealth observation does not match the complete snapshot")
		return
	}
	if !audit(r, tx, p, "WEALTH_SNAPSHOT_CREATE", "wealth_snapshot", id, nil, map[string]any{"observedAt": in.ObservedAt}) || tx.Commit(r.Context()) != nil {
		fail(w, 500, "unable to audit snapshot")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}

func (h *Handler) Observation(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	var id, accountID, institution, hint, value string
	var quantity, unit, unitPrice, observedDate *string
	err := h.pool.QueryRow(r.Context(), `SELECT id::text,COALESCE(resolved_wealth_account_id::text,''),institution,account_hint,observed_value_idr::text,quantity::text,unit,unit_price_idr::text,observed_date::text FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING'`, r.PathValue("id"), p.HouseholdID).Scan(&id, &accountID, &institution, &hint, &value, &quantity, &unit, &unitPrice, &observedDate)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "wealth observation not found")
		return
	}
	if err != nil {
		fail(w, 500, "unable to load wealth observation")
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "resolvedWealthAccountId": accountID, "institution": institution, "accountHint": hint, "observedValueIdr": value, "quantity": quantity, "unit": unit, "unitPriceIdr": unitPrice, "observedDate": observedDate})
}

func (h *Handler) CorrectSnapshot(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	var in snapshotInput
	if decode(r, &in) != nil || !validSnapshot(in) {
		fail(w, 400, "invalid wealth snapshot request")
		return
	}
	at, err := time.Parse(time.RFC3339, in.ObservedAt)
	if err != nil {
		fail(w, 400, "observedAt must be RFC3339")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		fail(w, 500, "unable to correct snapshot")
		return
	}
	defer tx.Rollback(r.Context())
	var stored time.Time
	err = tx.QueryRow(r.Context(), `SELECT observed_at FROM wealth_snapshot WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), p.HouseholdID).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "snapshot not found")
		return
	}
	if err != nil {
		fail(w, 500, "unable to correct snapshot")
		return
	}
	if !stored.Equal(at) {
		fail(w, 409, "observedAt is immutable")
		return
	}
	if !completeSnapshotSet(r, tx, r.PathValue("id"), in.Items) {
		fail(w, 400, "snapshot must preserve exactly its existing wealth accounts")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM wealth_snapshot_item WHERE snapshot_id=$1`, r.PathValue("id")); err != nil || !insertItems(r, tx, r.PathValue("id"), in.Items) {
		fail(w, 500, "unable to correct snapshot")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE wealth_snapshot SET updated_at=now() WHERE id=$1`, r.PathValue("id")); err != nil || !audit(r, tx, p, "WEALTH_SNAPSHOT_UPDATE", "wealth_snapshot", r.PathValue("id"), nil, map[string]any{"observedAt": in.ObservedAt}) || tx.Commit(r.Context()) != nil {
		fail(w, 500, "unable to audit snapshot")
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	var at time.Time
	err := h.pool.QueryRow(r.Context(), `SELECT observed_at FROM wealth_snapshot WHERE id=$1 AND household_id=$2`, r.PathValue("id"), p.HouseholdID).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "snapshot not found")
		return
	}
	if err != nil {
		fail(w, 500, "unable to get snapshot")
		return
	}
	h.snapshotOut(w, r, r.PathValue("id"), at)
}

func (h *Handler) Latest(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	var id string
	var at time.Time
	err := h.pool.QueryRow(r.Context(), `SELECT id,observed_at FROM wealth_snapshot WHERE household_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1`, p.HouseholdID).Scan(&id, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		jsonOut(w, 200, nil)
		return
	}
	if err != nil {
		fail(w, 500, "unable to get latest snapshot")
		return
	}
	h.snapshotOut(w, r, id, at)
}

func (h *Handler) snapshotOut(w http.ResponseWriter, r *http.Request, id string, at time.Time) {
	rows, err := h.pool.Query(r.Context(), `SELECT i.wealth_account_id,a.name,a.side,a.wealth_type,a.usage_role,i.value_idr::text,i.quantity::text,i.unit,i.unit_price_idr::text,i.source,i.note FROM wealth_snapshot_item i JOIN wealth_account a ON a.id=i.wealth_account_id WHERE i.snapshot_id=$1 ORDER BY a.side,a.usage_role,a.name,a.id`, id)
	if err != nil {
		fail(w, 500, "unable to get snapshot")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	assets, liabilities := big.NewInt(0), big.NewInt(0)
	for rows.Next() {
		var accountID, name, side, typ, role, value, source string
		var quantity, unit, price, note *string
		if rows.Scan(&accountID, &name, &side, &typ, &role, &value, &quantity, &unit, &price, &source, &note) != nil {
			fail(w, 500, "unable to get snapshot")
			return
		}
		amount, _ := new(big.Int).SetString(value, 10)
		if side == "ASSET" {
			assets.Add(assets, amount)
		} else {
			liabilities.Add(liabilities, amount)
		}
		items = append(items, map[string]any{"wealthAccountId": accountID, "name": name, "side": side, "wealthType": typ, "usageRole": role, "valueIdr": value, "quantity": quantity, "unit": unit, "unitPriceIdr": price, "source": source, "note": note})
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, "unable to get snapshot")
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "observedAt": at, "items": items, "assetTotalIdr": assets.String(), "liabilityTotalIdr": liabilities.String(), "netWorthIdr": new(big.Int).Sub(assets, liabilities).String()})
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,observed_at FROM wealth_snapshot WHERE household_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 2`, p.HouseholdID)
	if err != nil {
		fail(w, 500, "unable to calculate wealth summary")
		return
	}
	defer rows.Close()
	type ref struct {
		id string
		at time.Time
	}
	refs := make([]ref, 0, 2)
	for rows.Next() {
		var v ref
		if rows.Scan(&v.id, &v.at) != nil {
			fail(w, 500, "unable to calculate wealth summary")
			return
		}
		refs = append(refs, v)
	}
	if rows.Err() != nil {
		fail(w, 500, "unable to calculate wealth summary")
		return
	}
	if len(refs) == 0 {
		jsonOut(w, 200, nil)
		return
	}
	latest, err := h.snapshotSummary(r, refs[0].id, refs[0].at)
	if err != nil {
		fail(w, 500, "unable to calculate wealth summary")
		return
	}
	out := map[string]any{"latest": latest, "previous": nil, "netWorthChangeIdr": nil, "confirmedCashflowIdr": nil, "valuationAndOtherChangeIdr": nil}
	if len(refs) == 2 {
		previous, e := h.snapshotSummary(r, refs[1].id, refs[1].at)
		if e != nil {
			fail(w, 500, "unable to calculate wealth summary")
			return
		}
		var income, expense, refund string
		e = h.pool.QueryRow(r.Context(), `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='EXPENSE'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='REFUND'),0)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at>$2 AND transaction_at<=$3`, p.HouseholdID, refs[1].at, refs[0].at).Scan(&income, &expense, &refund)
		if e != nil {
			fail(w, 500, "unable to calculate wealth summary")
			return
		}
		cashflow, e := financialmath.CalculateCashflow(income, expense, refund)
		if e != nil {
			fail(w, 500, "unable to calculate wealth summary")
			return
		}
		change := subtract(latest["netWorthIdr"].(string), previous["netWorthIdr"].(string))
		out["previous"], out["netWorthChangeIdr"], out["confirmedCashflowIdr"], out["valuationAndOtherChangeIdr"] = previous, change, cashflow.Surplus, subtract(change, cashflow.Surplus)
	}
	jsonOut(w, 200, out)
}

func (h *Handler) snapshotSummary(r *http.Request, id string, at time.Time) (map[string]any, error) {
	rows, err := h.pool.Query(r.Context(), `SELECT a.side,a.usage_role,COALESCE(sum(i.value_idr),0)::text FROM wealth_snapshot_item i JOIN wealth_account a ON a.id=i.wealth_account_id WHERE i.snapshot_id=$1 GROUP BY a.side,a.usage_role ORDER BY a.side,a.usage_role`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets, liabilities := "0", "0"
	assetGroups, liabilityGroups := make([]map[string]string, 0), make([]map[string]string, 0)
	for rows.Next() {
		var side, role, amount string
		if err := rows.Scan(&side, &role, &amount); err != nil {
			return nil, err
		}
		group := map[string]string{"usageRole": role, "valueIdr": amount}
		if side == "ASSET" {
			assets = add(assets, amount)
			assetGroups = append(assetGroups, group)
		} else {
			liabilities = add(liabilities, amount)
			liabilityGroups = append(liabilityGroups, group)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "observedAt": at, "assetTotalIdr": assets, "liabilityTotalIdr": liabilities, "netWorthIdr": subtract(assets, liabilities), "assets": assetGroups, "liabilities": liabilityGroups}, nil
}

func (h *Handler) CycleRecaps(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(r)
	if !ok {
		fail(w, 400, "limit must be between 1 and 100")
		return
	}
	rows, err := h.pool.Query(r.Context(), `WITH cycles AS (SELECT se.id start_id,se.pay_date cycle_start,lead(se.id) OVER (ORDER BY se.pay_date,se.id) end_id,lead(se.pay_date) OVER (ORDER BY se.pay_date,se.id) cycle_end FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.household_id=$1 AND se.status='CONFIRMED' AND ss.is_primary) SELECT start_id,end_id,cycle_start,cycle_end FROM cycles WHERE cycle_end IS NOT NULL ORDER BY cycle_start DESC,start_id DESC LIMIT $2`, p.HouseholdID, limit)
	if err != nil {
		fail(w, 500, "unable to calculate cycle recaps")
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var startID, endID string
		var start, end time.Time
		if rows.Scan(&startID, &endID, &start, &end) != nil {
			fail(w, 500, "unable to calculate cycle recaps")
			return
		}
		var income, expense, refund, savings string
		err = h.pool.QueryRow(r.Context(), `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='EXPENSE'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='REFUND'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= ($2::date::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < ($3::date::timestamp AT TIME ZONE 'Asia/Jakarta')`, p.HouseholdID, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&income, &expense, &refund, &savings)
		if err != nil {
			fail(w, 500, "unable to calculate cycle recaps")
			return
		}
		cashflow, e := financialmath.CalculateCashflow(income, expense, refund)
		if e != nil {
			fail(w, 500, "unable to calculate cycle recaps")
			return
		}
		expense, surplus := cashflow.NetExpense, cashflow.Surplus
		residual := subtract(surplus, savings)
		destinations, attributions := make([]map[string]string, 0), make([]map[string]string, 0)
		destinationRows, e := h.pool.Query(r.Context(), `SELECT COALESCE(a.id::text,''),COALESCE(a.name,'Unlinked'),sum(t.amount)::text FROM transaction t LEFT JOIN wealth_account a ON a.id=t.related_wealth_account_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.transaction_at >= ($2::date::timestamp AT TIME ZONE 'Asia/Jakarta') AND t.transaction_at < ($3::date::timestamp AT TIME ZONE 'Asia/Jakarta') AND t.type='TRANSFER' AND t.purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE') GROUP BY a.id,a.name ORDER BY a.name,a.id`, p.HouseholdID, start.Format("2006-01-02"), end.Format("2006-01-02"))
		if e != nil {
			fail(w, 500, "unable to calculate cycle recaps")
			return
		}
		for destinationRows.Next() {
			var id, name, amount string
			if destinationRows.Scan(&id, &name, &amount) != nil {
				destinationRows.Close()
				fail(w, 500, "unable to calculate cycle recaps")
				return
			}
			destinations = append(destinations, map[string]string{"wealthAccountId": id, "name": name, "amountIdr": amount})
		}
		if destinationRows.Err() != nil {
			destinationRows.Close()
			fail(w, 500, "unable to calculate cycle recaps")
			return
		}
		destinationRows.Close()
		status := "NOT_REVIEWED"
		var caseID, basisIncome, basisExpense, basisSavings, allocated string
		e = h.pool.QueryRow(r.Context(), `SELECT c.id::text,c.basis_income_idr::text,c.basis_expense_idr::text,c.basis_savings_idr::text,COALESCE(sum(a.amount_idr),0)::text FROM cycle_residual_case c LEFT JOIN cycle_residual_allocation a ON a.cycle_residual_case_id=c.id WHERE c.household_id=$1 AND c.start_salary_event_id=$2 AND c.end_salary_event_id=$3 GROUP BY c.id`, p.HouseholdID, startID, endID).Scan(&caseID, &basisIncome, &basisExpense, &basisSavings, &allocated)
		if e == nil {
			allocationRows, x := h.pool.Query(r.Context(), `SELECT a.wealth_account_id::text,w.name,a.amount_idr::text FROM cycle_residual_allocation a JOIN wealth_account w ON w.id=a.wealth_account_id WHERE a.cycle_residual_case_id=$1 ORDER BY w.name,w.id,a.id`, caseID)
			if x != nil {
				fail(w, 500, "unable to calculate cycle recaps")
				return
			}
			for allocationRows.Next() {
				var id, name, amount string
				if allocationRows.Scan(&id, &name, &amount) != nil {
					allocationRows.Close()
					fail(w, 500, "unable to calculate cycle recaps")
					return
				}
				attributions = append(attributions, map[string]string{"wealthAccountId": id, "name": name, "amountIdr": amount})
			}
			if allocationRows.Err() != nil {
				allocationRows.Close()
				fail(w, 500, "unable to calculate cycle recaps")
				return
			}
			allocationRows.Close()
			if basisIncome != income || basisExpense != expense || basisSavings != savings {
				status = "STALE"
			} else if allocated == residual {
				status = "RESOLVED"
			} else {
				var reviewStatus string
				x = h.pool.QueryRow(r.Context(), `SELECT status FROM review_item WHERE cycle_residual_case_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, caseID).Scan(&reviewStatus)
				if x == nil && reviewStatus == "RESOLVED" {
					status = "LEFT_UNALLOCATED"
				} else {
					status = "CURRENT"
				}
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			fail(w, 500, "unable to calculate cycle recaps")
			return
		}
		out = append(out, map[string]any{"cycleStart": start.Format("2006-01-02"), "cycleEnd": end.Format("2006-01-02"), "income": income, "expense": expense, "cashflowSurplus": surplus, "savingsAllocated": savings, "rawResidual": residual, "savingsByDestination": destinations, "residualAttributions": attributions, "residualReviewStatus": status})
	}
	if rows.Err() != nil {
		fail(w, 500, "unable to calculate cycle recaps")
		return
	}
	jsonOut(w, 200, out)
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	h.listSnapshots(w, r, p.HouseholdID, true)
}

func (h *Handler) CurrentCycleSavings(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(w, r)
	if !ok {
		return
	}
	local := time.Now().In(clock.HouseholdLocation())
	var configured bool
	var start, end *time.Time
	if err := h.pool.QueryRow(r.Context(), `SELECT configured,starts_on,ends_on FROM salary_cycle_bounds($1,$2::date)`, p.HouseholdID, local.Format("2006-01-02")).Scan(&configured, &start, &end); err != nil || !configured || start == nil {
		jsonOut(w, 200, map[string]any{"configured": false, "savingsAllocated": "0", "savingsByDestination": []any{}})
		return
	}
	upper := local.AddDate(0, 0, 1)
	if end != nil {
		upper = *end
	}
	rows, err := h.pool.Query(r.Context(), `SELECT COALESCE(w.id::text,''),COALESCE(w.name,'Unlinked'),sum(t.amount)::text FROM transaction t LEFT JOIN wealth_account w ON w.id=t.related_wealth_account_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type='TRANSFER' AND t.purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE') AND t.transaction_at >= $2 AND t.transaction_at < $3 GROUP BY w.id,w.name ORDER BY w.name,w.id`, p.HouseholdID, *start, upper)
	if err != nil {
		fail(w, 500, "unable to calculate current cycle savings")
		return
	}
	defer rows.Close()
	total := new(big.Int)
	destinations := make([]map[string]string, 0)
	for rows.Next() {
		var id, name, amount string
		if rows.Scan(&id, &name, &amount) != nil {
			fail(w, 500, "unable to calculate current cycle savings")
			return
		}
		value, valid := new(big.Int).SetString(amount, 10)
		if !valid {
			fail(w, 500, "unable to calculate current cycle savings")
			return
		}
		total.Add(total, value)
		destinations = append(destinations, map[string]string{"wealthAccountId": id, "name": name, "amountIdr": amount})
	}
	if rows.Err() != nil {
		fail(w, 500, "unable to calculate current cycle savings")
		return
	}
	jsonOut(w, 200, map[string]any{"configured": true, "cycleStart": start.Format("2006-01-02"), "cycleEnd": upper.Format("2006-01-02"), "savingsAllocated": total.String(), "savingsByDestination": destinations})
}

func (h *Handler) listSnapshots(w http.ResponseWriter, r *http.Request, householdID string, totals bool) {
	selectSQL := `SELECT s.id,s.observed_at,s.created_at`
	if totals {
		selectSQL += `,COALESCE(sum(i.value_idr) FILTER(WHERE a.side='ASSET'),0)::text,COALESCE(sum(i.value_idr) FILTER(WHERE a.side='LIABILITY'),0)::text`
	}
	query := selectSQL + ` FROM wealth_snapshot s`
	if totals {
		query += ` JOIN wealth_snapshot_item i ON i.snapshot_id=s.id JOIN wealth_account a ON a.id=i.wealth_account_id`
	}
	query += ` WHERE s.household_id=$1`
	if totals {
		query += ` GROUP BY s.id`
	}
	query += ` ORDER BY s.observed_at DESC,s.id DESC`
	rows, err := h.pool.Query(r.Context(), query, householdID)
	if err != nil {
		fail(w, 500, "unable to list snapshots")
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var observed, created time.Time
		item := map[string]any{}
		if totals {
			var assets, liabilities string
			if rows.Scan(&id, &observed, &created, &assets, &liabilities) != nil {
				fail(w, 500, "unable to list snapshots")
				return
			}
			item["assetTotalIdr"], item["liabilityTotalIdr"] = assets, liabilities
			item["netWorthIdr"] = subtract(assets, liabilities)
		} else if rows.Scan(&id, &observed, &created) != nil {
			fail(w, 500, "unable to list snapshots")
			return
		}
		item["id"], item["observedAt"], item["createdAt"] = id, observed, created
		out = append(out, item)
	}
	if rows.Err() != nil {
		fail(w, 500, "unable to list snapshots")
		return
	}
	jsonOut(w, 200, out)
}

func validPatch(in accountPatchInput) bool {
	if in.Side != nil || in.WealthType != nil {
		return false
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return false
	}
	return in.Name != nil || in.Institution.Set || in.UsageRole != nil || in.OwnerUserID.Set || in.LinkedAccountID.Set || in.Active != nil
}

func validSide(side, typ, role string) bool {
	assets := map[string]bool{"BANK": true, "CASH": true, "EWALLET": true, "MUTUAL_FUND": true, "GOLD": true, "BROKERAGE": true, "DEPOSIT": true, "CRYPTO": true, "OTHER": true}
	return side == "ASSET" && assets[typ] && map[string]bool{"TRANSACTIONAL": true, "SAVINGS": true, "INVESTMENT": true, "OTHER": true}[role] || side == "LIABILITY" && (typ == "LOAN" || typ == "OTHER") && role == "OTHER"
}

func validSnapshot(in snapshotInput) bool {
	if in.ObservedAt == "" || len(in.Items) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range in.Items {
		if item.WealthAccountID == "" || seen[item.WealthAccountID] || !money(item.ValueIDR) || !map[string]bool{"MANUAL": true, "DOCUMENT": true, "SYSTEM": true}[item.Source] || item.Quantity != nil && !decimal(*item.Quantity) || item.UnitPriceIDR != nil && !money(*item.UnitPriceIDR) {
			return false
		}
		seen[item.WealthAccountID] = true
	}
	return true
}

func money(value string) bool {
	n, ok := new(big.Int).SetString(value, 10)
	return ok && n.Sign() >= 0 && n.String() == value && len(value) <= 20
}

func decimal(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 20 || len(parts) == 2 && (len(parts[1]) == 0 || len(parts[1]) > 10) {
		return false
	}
	for _, part := range parts {
		if _, ok := new(big.Int).SetString(part, 10); !ok {
			return false
		}
	}
	return true
}

func completeActiveSet(r *http.Request, tx pgx.Tx, householdID string, items []itemInput) bool {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].WealthAccountID
	}
	var activeCount, matchedCount int
	err := tx.QueryRow(r.Context(), `SELECT count(*)::int,count(*) FILTER(WHERE id=ANY($2::uuid[]))::int FROM wealth_account WHERE household_id=$1 AND active`, householdID, ids).Scan(&activeCount, &matchedCount)
	return err == nil && activeCount == len(items) && matchedCount == len(items)
}

func completeSnapshotSet(r *http.Request, tx pgx.Tx, snapshotID string, items []itemInput) bool {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].WealthAccountID
	}
	var existing, matched int
	err := tx.QueryRow(r.Context(), `SELECT count(*)::int,count(*) FILTER(WHERE wealth_account_id=ANY($2::uuid[]))::int FROM wealth_snapshot_item WHERE snapshot_id=$1`, snapshotID, ids).Scan(&existing, &matched)
	return err == nil && existing == len(items) && matched == len(items)
}

func applyObservation(r *http.Request, tx pgx.Tx, householdID, observationID string, items []itemInput) bool {
	var accountID, value string
	if err := tx.QueryRow(r.Context(), `SELECT resolved_wealth_account_id::text,observed_value_idr::text FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING' FOR UPDATE`, observationID, householdID).Scan(&accountID, &value); err != nil {
		return false
	}
	matched := false
	for _, item := range items {
		if item.WealthAccountID == accountID && item.ValueIDR == value {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	if _, err := tx.Exec(r.Context(), `UPDATE wealth_observation SET status='APPLIED',updated_at=now() WHERE id=$1`, observationID); err != nil {
		return false
	}
	_, err := tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolution_action='SNAPSHOT_CREATED',resolved_at=now(),updated_at=now() WHERE wealth_observation_id=$1 AND status IN ('OPEN','PENDING_SEND')`, observationID)
	return err == nil
}

func insertItems(r *http.Request, tx pgx.Tx, snapshotID string, items []itemInput) bool {
	for _, item := range items {
		_, err := tx.Exec(r.Context(), `INSERT INTO wealth_snapshot_item(snapshot_id,wealth_account_id,value_idr,quantity,unit,unit_price_idr,source,note) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, snapshotID, item.WealthAccountID, item.ValueIDR, item.Quantity, trim(item.Unit), item.UnitPriceIDR, item.Source, trim(item.Note))
		if err != nil {
			return false
		}
	}
	return true
}

func validRefs(r *http.Request, tx pgx.Tx, householdID string, ownerID, linkedID *string) bool {
	if ownerID != nil {
		var ok bool
		if tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM household_member hm JOIN "user" u ON u.id=hm.user_id WHERE hm.household_id=$1 AND hm.user_id=$2 AND u.active)`, householdID, *ownerID).Scan(&ok) != nil || !ok {
			return false
		}
	}
	if linkedID != nil {
		var ok bool
		if tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM account WHERE household_id=$1 AND id=$2 AND active)`, householdID, *linkedID).Scan(&ok) != nil || !ok {
			return false
		}
	}
	return true
}

func principal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		fail(w, 403, "household membership required")
		return auth.Principal{}, false
	}
	return p, true
}

func owner(w http.ResponseWriter, p auth.Principal) bool {
	if p.HouseholdRole != "OWNER" {
		fail(w, 403, "owner role required")
		return false
	}
	return true
}

func audit(r *http.Request, tx pgx.Tx, p auth.Principal, action, typ, id string, before, after any) bool {
	_, err := tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'USER',$2,$3,$4,$5,$6,$7)`, p.HouseholdID, p.UserID, action, typ, id, before, after)
	return err == nil
}

func trim(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func add(a, b string) string {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return new(big.Int).Add(x, y).String()
}

func subtract(a, b string) string {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return new(big.Int).Sub(x, y).String()
}

func decode(r *http.Request, value any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("content type")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple values")
	}
	return nil
}

func writeDBError(w http.ResponseWriter, err error, fallback string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail(w, 409, "wealth resource already exists")
		return
	}
	fail(w, 500, fallback)
}

func parseLimit(r *http.Request) (int, bool) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 12, true
	}
	limit, err := strconv.Atoi(value)
	return limit, err == nil && limit >= 1 && limit <= 100
}

func fail(w http.ResponseWriter, status int, message string) {
	jsonOut(w, status, map[string]string{"error": message})
}
func jsonOut(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
