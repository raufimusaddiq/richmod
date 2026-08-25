package budget

import (
	"testing"
	"time"
)

func TestBudgetMoneyAndMonthValidation(t *testing.T) {
	if !validMoney("2500000") || validMoney("0") || validMoney("2500.50") || validMoney("02500") {
		t.Fatal("whole positive IDR validation failed")
	}
	month, ok := parseMonth("2026-08", time.Time{})
	if !ok || month.Format("2006-01-02") != "2026-08-01" || month.Location().String() != "Asia/Jakarta" {
		t.Fatalf("unexpected month: %v", month)
	}
}

func TestBudgetArithmetic(t *testing.T) {
	if got := subtract("2000000", "750000"); got != "1250000" {
		t.Fatalf("remaining=%s", got)
	}
	if got := ratio("750000", "2000000"); got != "0.3750" {
		t.Fatalf("utilization=%s", got)
	}
}
