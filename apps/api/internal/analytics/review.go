package analytics

import (
	"errors"
	"net/http"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
)

type cycleReview = analyticscore.Facts

// CycleReview exposes the same authoritative engine used by analytical tools.
func (h *Handler) CycleReview(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start := r.URL.Query().Get("cycle_start")
	if start != "" {
		if _, err := time.Parse("2006-01-02", start); err != nil {
			writeJSON(w, 400, map[string]string{"error": "cycle_start must be YYYY-MM-DD"})
			return
		}
	}
	facts, err := analyticscore.Load(r.Context(), h.pool, household, start, cycleNow(h)())
	if errors.Is(err, analyticscore.ErrCycleNotFound) {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate cycle review"})
		return
	}
	writeJSON(w, 200, facts)
}
