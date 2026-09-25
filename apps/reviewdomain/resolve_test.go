package reviewdomain

import (
	"strings"
	"testing"
)

// The shared resolver owns exactly one canonical completion statement set.
// A regression that reintroduces channel-owned status writes in either adapter
// still leaves this contract intact, while a regression inside the shared
// operation fails here.
func TestSharedResolverOwnsCanonicalCompletion(t *testing.T) {
	for _, required := range []string{
		"status='RESOLVED'",
		"resolved_by_user_id=",
		"resolution_action=",
		"resolution_values=",
		"status IN ('PENDING_SEND','OPEN')",
	} {
		if !strings.Contains(resolveTransactionSQL, required) || !strings.Contains(resolveByIDSQL, required) {
			t.Fatalf("shared resolver completion is missing %q", required)
		}
	}
	// The API helper historically resolved every open item for a transaction;
	// the shared operation must not silently narrow to one row.
	if !strings.Contains(resolveTransactionSQL, "RETURNING id") || strings.Contains(resolveTransactionSQL, "LIMIT 1") {
		t.Fatal("transaction completion must resolve every open canonical item")
	}
	if !strings.Contains(resolveRequestSQL, "UPDATE review_request SET status='RESOLVED'") || !strings.Contains(resolveRequestSQL, "transaction_id=$2") {
		t.Fatal("shared resolver must resolve canonical and legacy transaction-linked projections")
	}
}
