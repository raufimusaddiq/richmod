package telegram

import (
	"testing"
	"time"
)

func TestRecordTransferTimeUsesLocalTime(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 30, 0, 0, jakartaLocation())
	date, clock := "TODAY", "10:15"
	got, err := resolveTime(now, &date, nil, &clock)
	if err != nil || got.Format("2006-01-02 15:04") != "2026-09-06 10:15" {
		t.Fatalf("got=%s err=%v", got, err)
	}
	clock = "25:99"
	if _, err := resolveTime(now, &date, nil, &clock); err == nil {
		t.Fatal("invalid local time accepted")
	}
}
