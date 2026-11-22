package financialmath

import "testing"

func TestCalculateCashflowRefundReducesExpense(t *testing.T) {
	value, err := CalculateCashflow("10000000", "5000000", "1000000")
	if err != nil || value.NetExpense != "4000000" || value.Surplus != "6000000" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestArithmeticHandlesInvalidInputsWithoutPanicking(t *testing.T) {
	if got := Add("1500", "2500"); got != "4000" {
		t.Fatalf("Add valid = %q, want 4000", got)
	}
	if got := Add("bad", "2500"); got != "2500" {
		t.Fatalf("Add invalid left = %q, want 2500", got)
	}
	if got := Add("1500", ""); got != "1500" {
		t.Fatalf("Add empty right = %q, want 1500", got)
	}
	if got := Subtract("5000", "bad"); got != "5000" {
		t.Fatalf("Subtract invalid right = %q, want 5000", got)
	}
	if got := Subtract("bad", "5000"); got != "-5000" {
		t.Fatalf("Subtract invalid left = %q, want -5000", got)
	}
	if ratio, ok := Ratio("1", "4"); !ok || ratio != "0.2500" {
		t.Fatalf("Ratio valid = %q,%v", ratio, ok)
	}
	for _, tc := range [][2]string{{"bad", "4"}, {"1", "bad"}, {"1", "0"}, {"1", "-2"}, {"", ""}} {
		if ratio, ok := Ratio(tc[0], tc[1]); ok || ratio != "" {
			t.Fatalf("Ratio(%q, %q) = %q,%v; want empty,false", tc[0], tc[1], ratio, ok)
		}
	}
}
