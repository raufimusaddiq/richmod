package insight

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

type Handler struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, now: time.Now} }

type categoryChange struct {
	Category              string `json:"category"`
	Current               string `json:"current"`
	PreviousThreeMonthAvg string `json:"previous_three_month_average"`
	Change                string `json:"change_vs_three_month_average"`
}

type facts struct {
	Period           string           `json:"period"`
	Currency         string           `json:"currency"`
	Income           string           `json:"income"`
	Expense          string           `json:"expense"`
	NetCashflow      string           `json:"net_cashflow"`
	SavingsRate      *string          `json:"savings_rate"`
	CategoryChanges  []categoryChange `json:"category_changes"`
	OpenReviewCount  int              `json:"open_review_count"`
	DataCompleteness string           `json:"data_completeness"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principal(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,to_char(period,'YYYY-MM'),status,input_metrics_json,gateway_route,model,prompt_version,generated_text,confidence::text,data_completeness::text,created_at,completed_at FROM insight WHERE household_id=$1 ORDER BY created_at DESC LIMIT 12`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list insights"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, period, status, prompt, completeness string
		var metrics json.RawMessage
		var route, model, text, confidence *string
		var created time.Time
		var completed *time.Time
		if err := rows.Scan(&id, &period, &status, &metrics, &route, &model, &prompt, &text, &confidence, &completeness, &created, &completed); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list insights"})
			return
		}
		result = append(result, map[string]any{"id": id, "period": period, "status": status, "metrics": metrics, "gatewayRoute": route, "model": model, "promptVersion": prompt, "text": text, "confidence": confidence, "dataCompleteness": completeness, "createdAt": created, "completedAt": completed})
	}
	writeJSON(w, 200, result)
}

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principal(w, r)
	if !ok {
		return
	}
	local := h.now().In(clock.HouseholdLocation())
	period := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
	if r.URL.Query().Get("period") == "cycle" {
		var anchored *time.Time
		if err := h.pool.QueryRow(r.Context(), `SELECT max(se.pay_date) FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED' AND se.pay_date <= $2::date`, household, local.Format("2006-01-02")).Scan(&anchored); err == nil && anchored != nil {
			period = *anchored
		}
	}
	var existing string
	err := h.pool.QueryRow(r.Context(), `SELECT id FROM insight WHERE household_id=$1 AND period=$2::date AND created_at>now()-interval '1 hour' ORDER BY created_at DESC LIMIT 1`, household, period).Scan(&existing)
	if err == nil {
		writeJSON(w, 200, map[string]string{"id": existing, "status": "EXISTING"})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 500, map[string]string{"error": "unable to check insight rate limit"})
		return
	}
	metrics, err := h.buildFacts(r, household, period)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to build deterministic insight facts"})
		return
	}
	raw, _ := json.Marshal(metrics)
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to request insight"})
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	if err := tx.QueryRow(r.Context(), `INSERT INTO insight(household_id,period,status,input_metrics_json,prompt_version,data_completeness,requested_by_user_id) VALUES($1,$2,'PENDING',$3::jsonb,'finance-insight-v1',$4,$5) RETURNING id`, household, period, string(raw), metrics.DataCompleteness, p.UserID).Scan(&id); err != nil {
		writeJSON(w, 409, map[string]string{"error": "an insight is already being generated"})
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO job(type,payload_json,max_attempts) VALUES('GENERATE_INSIGHT',jsonb_build_object('insight_id',$1::uuid),3); INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($2,'USER',$3,'REQUEST_INSIGHT','insight',$1,jsonb_build_object('period',$4::date,'data_completeness',$5::numeric))`, id, household, p.UserID, period, metrics.DataCompleteness); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to enqueue insight"})
		return
	}
	writeJSON(w, 202, map[string]string{"id": id, "status": "PENDING"})
}

func (h *Handler) buildFacts(r *http.Request, household string, period time.Time) (facts, error) {
	end := period.AddDate(0, 1, 0)
	if period.Day() != 1 {
		var next *time.Time
		_ = h.pool.QueryRow(r.Context(), `SELECT min(se.pay_date) FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED' AND se.pay_date>$2::date`, household, period.Format("2006-01-02")).Scan(&next)
		if next != nil { end = *next } else { local := h.now().In(clock.HouseholdLocation()); end = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, clock.HouseholdLocation()) }
	}
	previousStart := period.AddDate(0, -3, 0)
	var income, expense, categorizedExpense string
	var reviews int
	err := h.pool.QueryRow(r.Context(), `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME' AND status='CONFIRMED' AND transaction_at>=$2 AND transaction_at<$3),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' AND status='CONFIRMED' AND transaction_at>=$2 AND transaction_at<$3 THEN amount WHEN type='REFUND' AND status='CONFIRMED' AND transaction_at>=$2 AND transaction_at<$3 THEN -amount ELSE 0 END),0)::text,COALESCE(sum(amount) FILTER(WHERE type='EXPENSE' AND status='CONFIRMED' AND category_id IS NOT NULL AND transaction_at>=$2 AND transaction_at<$3),0)::text,(SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, household, period, end).Scan(&income, &expense, &categorizedExpense, &reviews)
	if err != nil {
		return facts{}, err
	}
	changes := make([]categoryChange, 0)
	rows, err := h.pool.Query(r.Context(), `SELECT c.name,COALESCE(sum(t.amount) FILTER(WHERE t.transaction_at>=$2 AND t.transaction_at<$3),0)::text,trunc(COALESCE(sum(t.amount) FILTER(WHERE t.transaction_at>=$4 AND t.transaction_at<$2),0)/3)::text FROM category c LEFT JOIN transaction t ON t.category_id=c.id AND t.household_id=$1 AND t.type='EXPENSE' AND t.status='CONFIRMED' WHERE c.household_id=$1 GROUP BY c.id,c.name HAVING COALESCE(sum(t.amount),0)>0 ORDER BY COALESCE(sum(t.amount) FILTER(WHERE t.transaction_at>=$2 AND t.transaction_at<$3),0) DESC LIMIT 10`, household, period, end, previousStart)
	if err != nil {
		return facts{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var value categoryChange
		if err := rows.Scan(&value.Category, &value.Current, &value.PreviousThreeMonthAvg); err != nil {
			return facts{}, err
		}
		value.Change = changeRatio(value.Current, value.PreviousThreeMonthAvg)
		changes = append(changes, value)
	}
	completeness := completenessRatio(categorizedExpense, expense, reviews)
	result := facts{Period: period.Format("2006-01"), Currency: "IDR", Income: income, Expense: expense, NetCashflow: subtract(income, expense), CategoryChanges: changes, OpenReviewCount: reviews, DataCompleteness: completeness}
	if value, ok := divide(result.NetCashflow, income); ok {
		result.SavingsRate = &value
	}
	return result, rows.Err()
}

func completenessRatio(categorized, expense string, reviews int) string {
	expenseValue, _ := new(big.Int).SetString(expense, 10)
	if expenseValue.Sign() <= 0 {
		if reviews == 0 {
			return "1.0000"
		}
		return "0.5000"
	}
	categoryValue, _ := new(big.Int).SetString(categorized, 10)
	base := new(big.Rat).SetFrac(categoryValue, expenseValue)
	if reviews > 0 {
		base.Mul(base, new(big.Rat).SetFrac64(9, 10))
	}
	if base.Cmp(big.NewRat(1, 1)) > 0 {
		base.SetInt64(1)
	}
	return base.FloatString(4)
}

func changeRatio(current, average string) string {
	avg, _ := new(big.Int).SetString(average, 10)
	if avg.Sign() <= 0 {
		return "unavailable"
	}
	cur, _ := new(big.Int).SetString(current, 10)
	return new(big.Rat).SetFrac(new(big.Int).Sub(cur, avg), avg).FloatString(4)
}

func subtract(left, right string) string {
	a, _ := new(big.Int).SetString(left, 10)
	b, _ := new(big.Int).SetString(right, 10)
	return new(big.Int).Sub(a, b).String()
}

func divide(left, right string) (string, bool) {
	a, _ := new(big.Int).SetString(left, 10)
	b, _ := new(big.Int).SetString(right, 10)
	if b.Sign() <= 0 {
		return "", false
	}
	return new(big.Rat).SetFrac(a, b).FloatString(4), true
}

func principal(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
