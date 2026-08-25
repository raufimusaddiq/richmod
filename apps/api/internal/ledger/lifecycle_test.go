package ledger

import "testing"

func TestTransactionStatusTransitions(t *testing.T) {
	tests := []struct {
		from, to string
		allowed  bool
	}{
		{"PENDING", "CONFIRMED", true},
		{"NEEDS_REVIEW", "CONFIRMED", true},
		{"CONFIRMED", "CONFIRMED", false},
		{"CONFIRMED", "VOIDED", true},
		{"VOIDED", "VOIDED", false},
		{"VOIDED", "CONFIRMED", false},
	}
	for _, test := range tests {
		if got := canTransition(test.from, test.to); got != test.allowed {
			t.Errorf("canTransition(%q,%q)=%t, want %t", test.from, test.to, got, test.allowed)
		}
	}
}
