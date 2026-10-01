package insight

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestPendingInsightRemainsIdempotent(t *testing.T) {
	if !strings.Contains(existingInsightQuery, "status='PENDING'") {
		t.Fatal("pending insight must return EXISTING instead of conflicting with unique index")
	}
	if !strings.Contains(existingInsightQuery, "period_kind") || !strings.Contains(existingInsightQuery, "period_start") {
		t.Fatal("cycle and calendar insight lookups must use deterministic metrics, not only the monthly storage key")
	}
	if strings.Contains(existingInsightQuery, "OR created_at") || !strings.Contains(existingInsightQuery, "status='SUCCEEDED'") {
		t.Fatal("failed insights must not block retry generation")
	}
	if insightPromptVersion != "cycle-analyst-v4" || !strings.Contains(existingInsightQuery, "prompt_version=$5") {
		t.Fatal("successful cached insights must match the current prompt version")
	}
	if !strings.Contains(existingInsightQuery, "input_metrics_json->>'period_end'=$6") {
		t.Fatal("cached commentary must match the measured cutoff")
	}
}

func TestListRejectsInvalidCycleBeforeDatabase(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/insights?cycle_start=bad", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: "u", HouseholdID: "h", HasHousehold: true}))
	w := httptest.NewRecorder()
	NewHandler(nil).List(w, r)
	if w.Code != 400 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
