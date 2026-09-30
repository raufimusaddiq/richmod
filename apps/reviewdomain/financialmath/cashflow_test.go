package financialmath

import "testing"

func TestCalculateCashflowRefundReducesExpense(t *testing.T) {
	value, err := CalculateCashflow("10000000", "5000000", "1000000")
	if err != nil || value.NetExpense != "4000000" || value.Surplus != "6000000" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}
