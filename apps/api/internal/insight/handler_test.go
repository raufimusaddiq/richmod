package insight

import "testing"

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
