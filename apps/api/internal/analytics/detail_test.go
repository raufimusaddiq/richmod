package analytics

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

func TestAnalyticsWholeIDRAddition(t *testing.T) {
	if got := add("750000", "250000"); got != "1000000" {
		t.Fatalf("sum=%s", got)
	}
}

func TestAnalyticsRangeUsesJakartaMonthBoundaries(t *testing.T) {
	handler := &Handler{now: func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, clock.HouseholdLocation()) }}
	start, end, err := handler.analyticsRange(httptest.NewRequest("GET", "/?range=3", nil))
	if err != nil {
		t.Fatal(err)
	}
	if start.Format("2006-01-02T15:04:05Z07:00") != "2026-06-01T00:00:00+07:00" || end.Format("2006-01-02T15:04:05Z07:00") != "2026-09-01T00:00:00+07:00" {
		t.Fatalf("range=%s to %s", start, end)
	}
	start, end, err = handler.analyticsRange(httptest.NewRequest("GET", "/?from=2025-10&to=2026-08", nil))
	if err != nil || monthsBetween(start, end) != 11 {
		t.Fatalf("custom range=%s to %s err=%v", start, end, err)
	}
	for _, raw := range []string{"range=5", "from=2026-01", "from=2026-09&to=2026-08", "from=2020-01&to=2026-08"} {
		if _, _, err := handler.analyticsRange(httptest.NewRequest("GET", "/?"+raw, nil)); err == nil {
			t.Fatalf("invalid range accepted: %s", raw)
		}
	}
}
