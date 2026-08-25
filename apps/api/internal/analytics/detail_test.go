package analytics

import "testing"

func TestAnalyticsWholeIDRAddition(t *testing.T) {
	if got := add("750000", "250000"); got != "1000000" {
		t.Fatalf("sum=%s", got)
	}
}
