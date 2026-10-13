package review

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// TestCanonicalActionsGolden pins the actions the Review Inbox offers for each
// canonical review kind, so moving where they are declared cannot change them.
func TestCanonicalActionsGolden(t *testing.T) {
	kinds := []string{
		"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE", "MISSING_AMOUNT",
		"CYCLE_RESIDUAL_ALLOCATION", "WEALTH_OBSERVATION_CONFIRMATION",
		"FINANCIAL_EMAIL_RESOLUTION", "UNKNOWN_BANK_TEMPLATE",
		"DOCUMENT_CLASSIFICATION", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE",
		"TRANSFER_CLASSIFICATION", "FINANCIAL_EMAIL_FACTS", "MISSING_TRANSACTION_DATE",
		"TRANSACTION_FACTS_MISSING", "UNKNOWN_MERCHANT", "NOT_A_KIND",
	}
	out := map[string][]string{}
	for _, kind := range kinds {
		out[kind] = reviewdec.InboxActions(kind)
	}
	got, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/canonical_actions.golden.json"
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical actions changed; review the diff and rerun with -update if intended\n%s", got)
	}
}
