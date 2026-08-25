package ledger

import (
	"net/http/httptest"
	"testing"
)

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

func TestTransactionFiltersValidateQueryParameters(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/transactions?from=2026-08-01&to=2026-08-31&type=EXPENSE&status=CONFIRMED&source=BANK_EMAIL&q=pamella", nil)
	filters, err := transactionFiltersFromRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if filters.Start == nil || filters.End == nil || filters.Type != "EXPENSE" || filters.Status != "CONFIRMED" || filters.Source != "BANK_EMAIL" || filters.Search != "pamella" {
		t.Fatalf("unexpected filters: %#v", filters)
	}
	if filters.End.Sub(*filters.Start).Hours() != 31*24 {
		t.Fatalf("exclusive end date = %s", filters.End)
	}
	for _, raw := range []string{"type=CRYPTO", "status=DELETED", "source=UNKNOWN", "from=25-08-2026", "from=2026-09-01&to=2026-08-01"} {
		if _, err := transactionFiltersFromRequest(httptest.NewRequest("GET", "/?"+raw, nil)); err == nil {
			t.Fatalf("invalid query accepted: %s", raw)
		}
	}
}
