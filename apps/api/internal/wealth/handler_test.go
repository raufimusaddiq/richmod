package wealth

import "testing"

func TestValidation(t *testing.T) {
	if !money("0") || !money("123") || money("01") || money("-1") || money("1.2") {
		t.Fatal("money validation")
	}
	if !validSide("ASSET", "GOLD", "INVESTMENT") || !validSide("LIABILITY", "LOAN", "OTHER") || validSide("LIABILITY", "LOAN", "SAVINGS") {
		t.Fatal("account validation")
	}
	if validSnapshot(snapshotInput{ObservedAt: "x", Items: []itemInput{{WealthAccountID: "a", ValueIDR: "1", Source: "MANUAL"}, {WealthAccountID: "a", ValueIDR: "2", Source: "MANUAL"}}}) {
		t.Fatal("duplicate account accepted")
	}
}

func TestExactIntegerArithmetic(t *testing.T) {
	if got := add("99999999999999999999", "1"); got != "100000000000000000000" {
		t.Fatalf("add = %s", got)
	}
	if got := subtract("1", "99999999999999999999"); got != "-99999999999999999998" {
		t.Fatalf("subtract = %s", got)
	}
}
