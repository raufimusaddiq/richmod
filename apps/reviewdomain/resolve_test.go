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
		"resolved_by_user_id=$2",
		"resolution_action=$3",
		"resolution_values=$4::jsonb",
		"status IN ('PENDING_SEND','OPEN')",
	} {
		if !strings.Contains(resolveSQL, required) || !strings.Contains(resolveByIDSQL, required) {
			t.Fatalf("shared resolver completion is missing %q", required)
		}
	}
	if !strings.Contains(resolveRequestSQL, "UPDATE review_request SET status='RESOLVED'") || !strings.Contains(resolveRequestSQL, "transaction_id=$2") {
		t.Fatal("shared resolver must resolve canonical and legacy transaction-linked projections")
	}
}
