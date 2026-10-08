package analytics

import (
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialmath"
)

func (h *Handler) CycleDaily(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	household := p.HouseholdID
	now := h.now().In(clock.HouseholdLocation())
	var start, end *time.Time
	var configured bool
	err := h.pool.QueryRow(r.Context(), `SELECT configured,starts_on,ends_on FROM salary_cycle_bounds($1,$2::date)`, household, now.Format("2006-01-02")).Scan(&configured, &start, &end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve salary cycle"})
		return
	}
	if !configured || start == nil {
		writeJSON(w, 200, map[string]any{"configured": false, "salary": nil, "remaining": nil, "daily": []any{}})
		return
	}
	if end == nil {
		e := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, clock.HouseholdLocation()).AddDate(0, 0, 1)
		end = &e
	}
	var salary string
	err = h.pool.QueryRow(r.Context(), `SELECT COALESCE(net_pay,0)::text FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id AND ss.household_id=se.household_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED' AND se.pay_date=$2::date ORDER BY se.created_at DESC LIMIT 1`, household, start).Scan(&salary)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 500, map[string]string{"error": "unable to load cycle salary"})
		return
	}
	rows, err := h.pool.Query(r.Context(), `WITH days AS (SELECT generate_series($2::date,$3::date-1,interval '1 day')::date AS day) SELECT to_char(days.day,'YYYY-MM-DD'),COALESCE(sum(t.amount) FILTER(WHERE t.type='INCOME'),0)::text,COALESCE(sum(t.amount) FILTER(WHERE t.type='EXPENSE'),0)::text,COALESCE(sum(t.amount) FILTER(WHERE t.type='REFUND'),0)::text,COALESCE(sum(CASE WHEN t.type='INCOME' THEN t.amount WHEN t.type='EXPENSE' THEN -t.amount WHEN t.type='REFUND' THEN t.amount ELSE 0 END),0)::text FROM days LEFT JOIN transaction t ON t.household_id=$1 AND t.status='CONFIRMED' AND (t.transaction_at AT TIME ZONE 'Asia/Jakarta')::date=days.day GROUP BY days.day ORDER BY days.day`, household, start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate daily cycle"})
		return
	}
	defer rows.Close()
	result := make([]map[string]string, 0)
	spent := big.NewInt(0)
	cumulative := big.NewInt(0)
	for rows.Next() {
		var period, income, expense, refund, net string
		if err := rows.Scan(&period, &income, &expense, &refund, &net); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to calculate daily cycle"})
			return
		}
		netExpense := financialmath.Subtract(expense, refund)
		amount, _ := new(big.Int).SetString(netExpense, 10)
		spent.Add(spent, amount)
		cumulative.Add(cumulative, amount)
		result = append(result, map[string]string{"period": period, "income": income, "grossExpense": expense, "expense": netExpense, "refund": refund, "netCashflow": net, "cumulativeExpense": cumulative.String()})
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate daily cycle"})
		return
	}
	days := calendarDays(*start, *end)
	elapsed := calendarDays(*start, now) + 1
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > days {
		elapsed = days
	}
	salaryValue := new(big.Rat)
	if salary != "" {
		if _, ok := salaryValue.SetString(salary); !ok {
			writeJSON(w, 500, map[string]string{"error": "invalid cycle salary"})
			return
		}
	}
	remaining := new(big.Rat).Sub(salaryValue, new(big.Rat).SetInt(spent))
	format := func(value *big.Rat) string {
		if value.IsInt() {
			return value.Num().String()
		}
		return value.FloatString(2)
	}
	writeJSON(w, 200, map[string]any{"configured": true, "daily": result, "salary": format(salaryValue), "spent": spent.String(), "remaining": format(remaining), "daysElapsed": elapsed, "daysTotal": days, "cycleStart": start.Format("2006-01-02"), "cycleEnd": end.Format("2006-01-02")})
}

func calendarDays(start, end time.Time) int {
	location := clock.HouseholdLocation()
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	endDate := time.Date(end.In(location).Year(), end.In(location).Month(), end.In(location).Day(), 0, 0, 0, 0, location)
	return int(endDate.Sub(startDate).Hours() / 24)
}
