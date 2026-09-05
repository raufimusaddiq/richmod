package analytics

import (
	"encoding/json"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
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
	local := h.now().In(clock.HouseholdLocation())
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
	end := start.AddDate(0, 1, 0)
	var cycleStart, cycleEnd *time.Time
	var cycleConfigured bool
	_ = h.pool.QueryRow(r.Context(), `SELECT configured,starts_on,ends_on FROM salary_cycle_bounds($1,$2::date)`, household, local.Format("2006-01-02")).Scan(&cycleConfigured, &cycleStart, &cycleEnd)
	if cycleConfigured && cycleStart != nil {
		start = *cycleStart
		if cycleEnd != nil {
			end = *cycleEnd
		} else {
			end = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, clock.HouseholdLocation())
		}
	}
	var income, expense string
	var review int
	err := h.pool.QueryRow(r.Context(), `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME' AND status='CONFIRMED'),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' AND status='CONFIRMED' THEN amount WHEN type='REFUND' AND status='CONFIRMED' THEN -amount ELSE 0 END),0)::text,(SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1 AND transaction_at >= $2 AND transaction_at < $3`, household, start, end).Scan(&income, &expense, &review)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate overview"})
		return
	}
	net := subtract(income, expense)
	var savings any = nil
	if value, ok := ratio(net, income); ok {
		savings = value
	}
	kind := "CALENDAR_MONTH"
	if cycleStart != nil {
		kind = "CURRENT_CYCLE"
	}
	writeJSON(w, 200, map[string]any{"period": start.Format("2006-01-02"), "periodKind": kind, "periodStart": start.Format("2006-01-02"), "periodEnd": end.Format("2006-01-02"), "currency": "IDR", "income": income, "expense": expense, "netCashflow": net, "savingsRate": savings, "reviewCount": review})
}
func subtract(a, b string) string {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return new(big.Int).Sub(x, y).String()
}
func ratio(n, d string) (string, bool) {
	num, _ := new(big.Int).SetString(n, 10)
	den, _ := new(big.Int).SetString(d, 10)
	if den.Sign() <= 0 {
		return "", false
	}
	return new(big.Rat).SetFrac(num, den).FloatString(4), true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
