package document

import (
	"testing"
	"time"
)

func TestSalaryCycleUsesConfirmedAnchors(t *testing.T) {
	loc := jakarta()
	a := time.Date(2026, 8, 25, 12, 0, 0, 0, loc)
	b := time.Date(2026, 9, 24, 12, 0, 0, 0, loc)
	start, end, ok := SalaryCycle([]time.Time{a, b}, time.Date(2026, 9, 1, 0, 0, 0, 0, loc))
	if !ok || !start.Equal(a) || end == nil || !end.Equal(b) {
		t.Fatalf("got %v %v %v", start, end, ok)
	}
	_, end, ok = SalaryCycle([]time.Time{a, b}, time.Date(2026, 10, 1, 0, 0, 0, 0, loc))
	if !ok || end != nil {
		t.Fatalf("current cycle should remain open: %v %v", ok, end)
	}
}
