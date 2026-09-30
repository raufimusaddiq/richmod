package clock

import (
	"testing"
	"time"
)

func TestHouseholdLocationIsGMTPlusSeven(t *testing.T) {
	instant := time.Date(2026, time.August, 25, 0, 0, 0, 0, time.UTC)
	_, offset := instant.In(HouseholdLocation()).Zone()
	if offset != 7*60*60 {
		t.Fatalf("Asia/Jakarta offset = %d, want %d", offset, 7*60*60)
	}
}
