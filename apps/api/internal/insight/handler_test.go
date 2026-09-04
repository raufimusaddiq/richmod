package insight

import (
	"strings"
	"testing"
)

func TestInsightFactArithmetic(t *testing.T) {
	if got := completenessRatio("750000", "1000000", 0); got != "0.7500" {
		t.Fatalf("completeness=%s", got)
	}
	if got := completenessRatio("750000", "1000000", 2); got != "0.6750" {
		t.Fatalf("review-adjusted completeness=%s", got)
	}
	if got := changeRatio("120", "100"); got != "0.2000" {
		t.Fatalf("change=%s", got)
	}
	if got := changeRatio("120", "0"); got != "unavailable" {
		t.Fatalf("zero baseline=%s", got)
	}
}

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
	if insightPromptVersion != "finance-insight-v2" || !strings.Contains(existingInsightQuery, "prompt_version=$5") {
		t.Fatal("successful cached insights must match the current prompt version")
	}
}
