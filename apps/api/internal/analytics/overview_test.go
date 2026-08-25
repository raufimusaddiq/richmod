package analytics

import "testing"

func TestOverviewArithmetic(t *testing.T) {
	if got := subtract("16000000", "7300000"); got != "8700000" {
		t.Fatalf("net=%s", got)
	}
	if got, ok := ratio("8700000", "16000000"); !ok || got != "0.5438" {
		t.Fatalf("savings=%s,%t", got, ok)
	}
	if _, ok := ratio("0", "0"); ok {
		t.Fatal("zero income must return unavailable")
	}
}
