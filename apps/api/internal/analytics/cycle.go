package analytics

import (
	"net/http"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

// Cycle returns the current salary-anchored financial period. Only confirmed
// events for the active primary salary source can open a cycle; without one,
// callers receive an explicit calendar fallback instead of a guessed payday.
func (h *Handler) Cycle(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	household := p.Memberships[0].HouseholdID
	now := time.Now().In(clock.HouseholdLocation())
	var start *time.Time
	var end *time.Time
	err := h.pool.QueryRow(r.Context(), `
		WITH anchors AS (
			SELECT se.pay_date
			FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id
			WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED'
		)
		SELECT (SELECT max(pay_date) FROM anchors WHERE pay_date <= $2::date),
		       (SELECT min(pay_date) FROM anchors WHERE pay_date > $2::date)`, household, now.Format("2006-01-02")).Scan(&start, &end)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to resolve salary cycle"})
		return
	}
	if start == nil {
		calendarStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
		writeJSON(w, http.StatusOK, map[string]any{"kind": "CALENDAR_MONTH", "start": calendarStart.Format("2006-01-02"), "end": calendarStart.AddDate(0, 1, 0).Format("2006-01-02"), "open": false, "configured": false})
		return
	}
	result := map[string]any{"kind": "CURRENT_CYCLE", "start": start.Format("2006-01-02"), "open": end == nil, "configured": true}
	if end != nil {
		result["end"] = end.Format("2006-01-02")
	}
	writeJSON(w, http.StatusOK, result)
}
