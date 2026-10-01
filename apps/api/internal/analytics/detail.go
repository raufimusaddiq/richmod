package analytics

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialmath"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialperiod"
)

type monthlyValue struct {
	Period  string `json:"period"`
	Income  string `json:"income"`
	Expense string `json:"expense"`
	Refund  string `json:"refund"`
	Net     string `json:"netCashflow"`
}

func (h *Handler) Cashflow(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start, end, err := h.analyticsRange(household, r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.monthly(r.Context(), household, start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate cashflow"})
		return
	}
	writeJSON(w, 200, rows)
}

func (h *Handler) Spending(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start, end, err := h.analyticsRange(household, r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.monthly(r.Context(), household, start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate spending"})
		return
	}
	result := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		// Legacy expense is already net; refund is separate gross provenance.
		result = append(result, map[string]string{"period": row.Period, "expense": row.Expense, "refund": row.Refund, "netSpending": row.Expense})
	}
	writeJSON(w, 200, result)
}

func (h *Handler) monthly(ctx context.Context, household string, start, end time.Time) ([]monthlyValue, error) {
	rows, err := h.pool.Query(ctx, `
		WITH months AS (SELECT generate_series(
			date_trunc('month',$2::timestamptz AT TIME ZONE 'Asia/Jakarta'),
			date_trunc('month',($3::timestamptz-interval '1 microsecond') AT TIME ZONE 'Asia/Jakarta'),interval '1 month') AS month)
		SELECT to_char(months.month,'YYYY-MM'),
		       COALESCE(sum(t.amount) FILTER(WHERE t.type='INCOME'),0)::text,
		       COALESCE(sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END),0)::text,
		       COALESCE(sum(t.amount) FILTER(WHERE t.type='REFUND'),0)::text
		FROM months LEFT JOIN transaction t ON t.household_id=$1 AND t.status='CONFIRMED'
		 AND t.transaction_at>=GREATEST(months.month AT TIME ZONE 'Asia/Jakarta',$2::timestamptz)
		 AND t.transaction_at<LEAST((months.month+interval '1 month') AT TIME ZONE 'Asia/Jakarta',$3::timestamptz)
		GROUP BY months.month ORDER BY months.month`, household, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]monthlyValue, 0, 12)
	for rows.Next() {
		var value monthlyValue
		if err := rows.Scan(&value.Period, &value.Income, &value.Expense, &value.Refund); err != nil {
			return nil, err
		}
		value.Net = subtract(value.Income, value.Expense)
		result = append(result, value)
	}
	return result, rows.Err()
}

type rankedValue struct {
	ID     *string `json:"id"`
	Name   string  `json:"name"`
	Amount string  `json:"amount"`
	Share  string  `json:"share"`
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start, end, err := h.analyticsRange(household, r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT c.id,COALESCE(c.name,'Belum dikategorikan'),sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END)::text FROM transaction t LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN ('EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 GROUP BY c.id,c.name HAVING sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END)<>0 ORDER BY sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END) DESC`, household, start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate category spending"})
		return
	}
	defer rows.Close()
	result := make([]rankedValue, 0)
	total := "0"
	for rows.Next() {
		var value rankedValue
		if err := rows.Scan(&value.ID, &value.Name, &value.Amount); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to calculate category spending"})
			return
		}
		total = add(total, value.Amount)
		result = append(result, value)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate category spending"})
		return
	}
	for index := range result {
		result[index].Share, _ = ratio(result[index].Amount, total)
	}
	writeJSON(w, 200, result)
}

func (h *Handler) Merchants(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start, end, err := h.analyticsRange(household, r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.pool.Query(r.Context(), `WITH merchants AS (
		SELECT m.id,COALESCE(m.normalized_name,NULLIF(t.counterparty_name,''),'Merchant tidak diketahui') AS name,
		sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END) AS amount
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
		WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN ('EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3
		GROUP BY m.id,2
	) SELECT id,name,amount::text,(SELECT COALESCE(sum(amount),0)::text FROM merchants)
	FROM merchants WHERE amount>0 ORDER BY amount DESC,name,id LIMIT 10`, household, start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate merchant spending"})
		return
	}
	defer rows.Close()
	result := make([]rankedValue, 0)
	total := "0"
	for rows.Next() {
		var value rankedValue
		if err := rows.Scan(&value.ID, &value.Name, &value.Amount, &total); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to calculate merchant spending"})
			return
		}
		result = append(result, value)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate merchant spending"})
		return
	}
	for index := range result {
		result[index].Share, _ = ratio(result[index].Amount, total)
	}
	writeJSON(w, 200, result)
}

func (h *Handler) Members(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start, end, err := h.analyticsRange(household, r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT u.id,COALESCE(u.display_name,'Otomatis / rumah tangga'),sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END)::text FROM transaction t LEFT JOIN household_member hm ON hm.user_id=t.created_by_user_id AND hm.household_id=t.household_id LEFT JOIN "user" u ON u.id=hm.user_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN ('EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 GROUP BY u.id,u.display_name HAVING sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END)<>0 ORDER BY sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END) DESC`, household, start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate member spending"})
		return
	}
	defer rows.Close()
	result := make([]rankedValue, 0)
	total := "0"
	for rows.Next() {
		var value rankedValue
		if err := rows.Scan(&value.ID, &value.Name, &value.Amount); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to calculate member spending"})
			return
		}
		total = add(total, value.Amount)
		result = append(result, value)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate member spending"})
		return
	}
	for index := range result {
		result[index].Share, _ = ratio(result[index].Amount, total)
	}
	writeJSON(w, 200, result)
}

func (h *Handler) currentPeriod() (time.Time, time.Time) {
	local := h.now().In(clock.HouseholdLocation())
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
	return start, start.AddDate(0, 1, 0)
}

func (h *Handler) analyticsRange(household string, r *http.Request) (time.Time, time.Time, error) {
	local := h.now().In(clock.HouseholdLocation())
	current := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "current_cycle" {
		cycle, err := financialperiod.Current(r.Context(), h.pool, household, local)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return cycle.Start, cycle.End, nil
	}
	if period != "" && period != "calendar" && period != "custom" {
		return time.Time{}, time.Time{}, fmt.Errorf("period must be current_cycle, calendar, or custom")
	}
	from, to := strings.TrimSpace(r.URL.Query().Get("from")), strings.TrimSpace(r.URL.Query().Get("to"))
	if from != "" || to != "" {
		if from == "" || to == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("from and to months are both required")
		}
		start, err := time.ParseInLocation("2006-01", from, clock.HouseholdLocation())
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from month")
		}
		last, err := time.ParseInLocation("2006-01", to, clock.HouseholdLocation())
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to month")
		}
		end := last.AddDate(0, 1, 0)
		if !start.Before(end) || end.After(current.AddDate(0, 2, 0)) || monthsBetween(start, end) > 24 {
			return time.Time{}, time.Time{}, fmt.Errorf("custom range must span 1 to 24 months")
		}
		return start, end, nil
	}
	months := 6
	if raw := strings.TrimSpace(r.URL.Query().Get("range")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || (value != 3 && value != 6 && value != 12) {
			return time.Time{}, time.Time{}, fmt.Errorf("range must be 3, 6, or 12 months")
		}
		months = value
	}
	return current.AddDate(0, -(months - 1), 0), current.AddDate(0, 1, 0), nil
}

func monthsBetween(start, end time.Time) int {
	return (end.Year()-start.Year())*12 + int(end.Month()-start.Month())
}

func analyticsHousehold(w http.ResponseWriter, r *http.Request) (string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return "", false
	}
	return p.HouseholdID, true
}

func add(left, right string) string {
	return financialmath.Add(left, right)
}
