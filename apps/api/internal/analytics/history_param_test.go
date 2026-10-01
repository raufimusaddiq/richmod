package analytics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

// Validation runs before any database access, so a nil pool proves the 400 path.
func TestCycleReviewRejectsInvalidHistory(t *testing.T) {
	h := NewCycleHandler(nil, func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, clock.HouseholdLocation()) })
	for _, value := range []string{"0", "13", "-1", "abc", "1.5", "999"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cycle-review?history="+value, nil)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: "user", Memberships: []auth.Membership{{HouseholdID: "household", Role: "OWNER"}}}))
		w := httptest.NewRecorder()
		h.CycleReview(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("history=%s status=%d body=%s", value, w.Code, w.Body.String())
		}
	}
}
