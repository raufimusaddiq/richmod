package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialmath"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialperiod"
)

type Handler struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, now: time.Now} }

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	household := p.HouseholdID
	period, err := financialperiod.Current(r.Context(), h.pool, household, h.now())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve overview period"})
		return
	}
	var income, expense, refund, savingsAllocated string
	var review, confirmedCount int
	err = h.pool.QueryRow(r.Context(), `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME' AND status='CONFIRMED'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='EXPENSE' AND status='CONFIRMED'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='REFUND' AND status='CONFIRMED'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND status='CONFIRMED' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0)::text,(SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'),count(*) FILTER(WHERE status='CONFIRMED' AND type IN ('INCOME','EXPENSE','REFUND','TRANSFER'))::int FROM transaction WHERE household_id=$1 AND transaction_at >= $2 AND transaction_at < $3`, household, period.Start, period.End).Scan(&income, &expense, &refund, &savingsAllocated, &review, &confirmedCount)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate overview"})
		return
	}
	cashflow, err := financialmath.CalculateCashflow(income, expense, refund)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate overview"})
		return
	}
	expense, net := cashflow.NetExpense, cashflow.Surplus
	unallocatedSurplus := subtract(net, savingsAllocated)
	var savings, savingsAllocationRate any = nil, nil
	if value, ok := ratio(net, income); ok {
		savings = value
	}
	if value, ok := ratio(savingsAllocated, income); ok {
		savingsAllocationRate = value
	}
	var previousExpense, previousDays, expenseDelta any = nil, nil, nil
	if prev, ok, err := h.previousPeriod(r.Context(), household, period); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate overview"})
		return
	} else if ok {
		var prevExpense, prevRefund string
		if err := h.pool.QueryRow(r.Context(), `SELECT COALESCE(sum(amount) FILTER(WHERE type='EXPENSE'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='REFUND'),0)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, household, prev.Start, prev.End).Scan(&prevExpense, &prevRefund); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to calculate overview"})
			return
		}
		net := subtract(prevExpense, prevRefund)
		previousExpense, previousDays, expenseDelta = net, prev.Days, subtract(expense, net)
	}
	writeJSON(w, 200, map[string]any{"period": period.Start.Format("2006-01-02"), "periodKind": period.Kind, "periodStart": period.Start.Format("2006-01-02"), "periodEnd": period.End.Format("2006-01-02"), "currency": "IDR", "income": income, "expense": expense, "netCashflow": net, "savingsRate": savings, "savingsAllocated": savingsAllocated, "savingsAllocationRate": savingsAllocationRate, "unallocatedSurplus": unallocatedSurplus, "reviewCount": review, "confirmedCount": confirmedCount, "previousExpense": previousExpense, "previousDays": previousDays, "expenseDelta": expenseDelta})
}
type priorPeriod struct {
	Start, End time.Time
	Days       int
}

func (h *Handler) previousPeriod(ctx context.Context, household string, period financialperiod.CurrentPeriod) (priorPeriod, bool, error) {
	if period.Kind == "CURRENT_CYCLE" {
		var configured bool
		var startsOn *string
		dayBefore := period.Start.AddDate(0, 0, -1).Format("2006-01-02")
		if err := h.pool.QueryRow(ctx, `SELECT configured,starts_on::text FROM salary_cycle_bounds($1,$2::date)`, household, dayBefore).Scan(&configured, &startsOn); err != nil {
			return priorPeriod{}, false, err
		}
		if !configured || startsOn == nil {
			return priorPeriod{}, false, nil
		}
		start, err := time.ParseInLocation("2006-01-02", *startsOn, clock.HouseholdLocation())
		if err != nil || !start.Before(period.Start) {
			return priorPeriod{}, false, err
		}
		return priorPeriod{Start: start, End: period.Start, Days: daysBetween(start, period.Start)}, true, nil
	}
	start := period.Start.AddDate(0, -1, 0)
	var hasRows bool
	if err := h.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND type IN ('INCOME','EXPENSE','REFUND') AND transaction_at >= $2 AND transaction_at < $3)`, household, start, period.Start).Scan(&hasRows); err != nil || !hasRows {
		return priorPeriod{}, false, err
	}
	return priorPeriod{Start: start, End: period.Start, Days: daysBetween(start, period.Start)}, true, nil
}

func daysBetween(start, end time.Time) int {
	return int(end.Sub(start).Round(24*time.Hour) / (24 * time.Hour))
}

func subtract(a, b string) string {
	return financialmath.Subtract(a, b)
}
func ratio(n, d string) (string, bool) {
	return financialmath.Ratio(n, d)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
