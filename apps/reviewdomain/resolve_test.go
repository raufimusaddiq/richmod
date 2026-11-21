package reviewdomain

import (
	"strings"
	"testing"
)

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
	if !strings.Contains(resolveTransactionSQL, "RETURNING id") || strings.Contains(resolveTransactionSQL, "LIMIT 1") {
		t.Fatal("transaction completion must resolve every open canonical item")
	}
	if !strings.Contains(resolveRequestSQL, "UPDATE review_request SET status='RESOLVED'") || !strings.Contains(resolveRequestSQL, "transaction_id=$2") {
		t.Fatal("shared resolver must resolve canonical and legacy transaction-linked projections")
	}
	if !strings.Contains(resolveRequestByItemSQL, "review_item_id=ANY($1::uuid[])") || strings.Contains(resolveRequestByItemSQL, "transaction_id") {
		t.Fatal("exact-item resolution must not close sibling transaction projections")
	}
}
