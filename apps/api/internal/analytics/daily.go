package analytics

import (
	"net/http"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

func (h *Handler) CycleDaily(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 { writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"}); return }
	household := p.Memberships[0].HouseholdID
	now := time.Now().In(clock.HouseholdLocation())
	var start, end *time.Time
	err := h.pool.QueryRow(r.Context(), `WITH anchors AS (SELECT se.pay_date FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED') SELECT (SELECT max(pay_date) FROM anchors WHERE pay_date <= $2::date),(SELECT min(pay_date) FROM anchors WHERE pay_date > $2::date)`, household, now.Format("2006-01-02")).Scan(&start, &end)
	if err != nil { writeJSON(w, 500, map[string]string{"error": "unable to resolve salary cycle"}); return }
	if start == nil { s := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation()); e := s.AddDate(0, 1, 0); start, end = &s, &e }
	if end == nil { e := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, clock.HouseholdLocation()).AddDate(0, 0, 1); end = &e }
	rows, err := h.pool.Query(r.Context(), `WITH days AS (SELECT generate_series($2::date,$3::date-1,interval '1 day')::date AS day) SELECT to_char(days.day,'YYYY-MM-DD'),COALESCE(sum(t.amount) FILTER(WHERE t.type='INCOME'),0)::text,COALESCE(sum(t.amount) FILTER(WHERE t.type='EXPENSE'),0)::text,COALESCE(sum(CASE WHEN t.type='INCOME' THEN t.amount WHEN t.type IN ('EXPENSE','REFUND') THEN -t.amount ELSE 0 END),0)::text FROM days LEFT JOIN transaction t ON t.household_id=$1 AND t.status='CONFIRMED' AND (t.transaction_at AT TIME ZONE 'Asia/Jakarta')::date=days.day GROUP BY days.day ORDER BY days.day`, household, start, end)
	if err != nil { writeJSON(w, 500, map[string]string{"error": "unable to calculate daily cycle"}); return }
	defer rows.Close(); result := make([]map[string]string, 0)
	for rows.Next() { var period, income, expense, net string; if err := rows.Scan(&period,&income,&expense,&net); err != nil { writeJSON(w,500,map[string]string{"error":"unable to calculate daily cycle"}); return }; result = append(result,map[string]string{"period":period,"income":income,"expense":expense,"netCashflow":net}) }
	writeJSON(w, 200, result)
}
