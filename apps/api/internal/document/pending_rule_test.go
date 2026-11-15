package document

import (
	"os"
	"strings"
	"testing"
)

// The migration repairs stored rows with the rule the API uses to show them;
// two copies of that rule must not drift.
func TestMigration00099EmbedsTheAPIPendingRule(t *testing.T) {
	raw, err := os.ReadFile("../../../../db/migrations/00099_settle_stale_needs_review_documents.sql")
	if err != nil {
		t.Fatal(err)
	}
	squash := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if !strings.Contains(squash(string(raw)), squash(documentPendingSQL)) {
		t.Fatal("migration 00099 does not embed documentPendingSQL verbatim")
	}
}
