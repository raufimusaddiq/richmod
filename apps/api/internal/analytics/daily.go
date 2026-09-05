package analytics

import (
	"math/big"
	"net/http"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
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
	_ = h.pool.QueryRow(r.Context(), `SELECT COALESCE(net_pay,0)::text FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED' AND se.pay_date=$2::date ORDER BY se.created_at DESC LIMIT 1`, household, start).Scan(&salary)
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
		amount, _ := new(big.Int).SetString(expense, 10)
		spent.Add(spent, amount)
		cumulative.Add(cumulative, amount)
		result = append(result, map[string]string{"period": period, "income": income, "expense": expense, "refund": refund, "netCashflow": net, "cumulativeExpense": cumulative.String()})
	}
	days := calendarDays(*start, *end)
	elapsed := calendarDays(*start, now) + 1
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > days {
		elapsed = days
	}
	remaining := new(big.Int)
	salaryValue, ok := new(big.Int).SetString(salary, 10)
	if !ok {
		salaryValue = big.NewInt(0)
	}
	remaining.Sub(salaryValue, spent)
	writeJSON(w, 200, map[string]any{"configured": true, "daily": result, "salary": salaryValue.String(), "spent": spent.String(), "remaining": remaining.String(), "daysElapsed": elapsed, "daysTotal": days, "cycleStart": start.Format("2006-01-02"), "cycleEnd": end.Format("2006-01-02")})
}

func calendarDays(start, end time.Time) int {
	location := clock.HouseholdLocation()
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	endDate := time.Date(end.In(location).Year(), end.In(location).Month(), end.In(location).Day(), 0, 0, 0, 0, location)
	return int(endDate.Sub(startDate).Hours() / 24)
}
