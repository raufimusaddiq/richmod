package insight

import (
	"strings"
	"testing"
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
	if insightPromptVersion != "cycle-analyst-v3" || !strings.Contains(existingInsightQuery, "prompt_version=$5") {
		t.Fatal("successful cached insights must match the current prompt version")
	}
}
