package settings

import "testing"

func TestJagoAccountPolicyRemainsSpendingOnly(t *testing.T) {
	if message := accountPolicyError("", "Bank Jago", "FULL_LEDGER"); message == "" {
		t.Fatal("new Jago account accepted non-spending policy")
	}
	if message := accountPolicyError("Bank Jago", "Renamed Bank", "SPENDING_ONLY"); message == "" {
		t.Fatal("existing Jago account could lose its identity")
	}
	if message := accountPolicyError("Bank Jago", "Bank Jago", "SPENDING_ONLY"); message != "" {
		t.Fatalf("valid Jago policy rejected: %s", message)
	}
	if message := accountPolicyError("Cash", "Cash household", "FULL_LEDGER"); message != "" {
		t.Fatalf("unrelated account rejected: %s", message)
	}
}
