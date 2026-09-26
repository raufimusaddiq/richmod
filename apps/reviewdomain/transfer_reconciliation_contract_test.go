package reviewdomain

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The shared operation is the only place allowed to mutate a reconciliation
// case or confirm its candidate. Adapters pass a candidate id/action; if a
// surface relearns the case update it can drift from the shared policy again
// (UIRC-02 A).
func TestReconcileTransferOwnsCaseMutation(t *testing.T) {
	source := sourceFile(t, "transfer_reconciliation.go")
	if !strings.Contains(source, "FOR UPDATE OF ri,trc") {
		t.Fatal("case lock missing from shared reconciliation")
	}
	for _, want := range []string{"UPDATE transfer_reconciliation_case", "UPDATE review_item SET status='RESOLVED'", "INSERT INTO transaction_evidence"} {
		if !strings.Contains(source, want) {
			t.Fatalf("shared reconciliation lost %q", want)
		}
	}
}

func TestTransferAdaptersDelegateReconciliation(t *testing.T) {
	adapters := map[string]string{
		"../api/internal/review/canonical.go":                       "reviewdomain.ReconcileTransfer",
		"../worker/internal/telegram/review.go":                     "reviewdomain.ReconcileTransfer",
		"../worker/internal/telegram/agent_review_mutations.go":     "reviewdomain.ReconcileTransfer",
		"../worker/internal/telegram/agent_review_binding_guard.go": "reviewdomain.ReconcileTransfer",
	}
	caseWrite := regexp.MustCompile(`(?s)UPDATE\s+transfer_reconciliation_case`)
	for path, want := range adapters {
		body := sourceFile(t, path)
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not call the shared reconciliation", path)
		}
		if caseWrite.MatchString(body) {
			t.Fatalf("%s still mutates the reconciliation case directly", path)
		}
	}
}

func sourceFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
