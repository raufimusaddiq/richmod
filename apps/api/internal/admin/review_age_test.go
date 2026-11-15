package admin

import (
	"testing"
	"time"
)

func TestReviewAgeMsFreezesAtResolution(t *testing.T) {
	created := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	resolved := created.Add(61 * time.Minute)
	if got := reviewAgeMs(created, &resolved, created.Add(10*time.Hour)); got != (61 * time.Minute).Milliseconds() {
		t.Fatalf("resolved age must not grow, got %d", got)
	}
	if got := reviewAgeMs(created, nil, created.Add(2*time.Hour)); got != (2 * time.Hour).Milliseconds() {
		t.Fatalf("open age must grow, got %d", got)
	}
	if got := reviewAgeMs(created, &created, created.Add(-time.Hour)); got != 0 {
		t.Fatalf("negative age clamps to 0, got %d", got)
	}
}
