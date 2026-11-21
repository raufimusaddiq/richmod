package telegram

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func TestAgentReviewActionsGolden(t *testing.T) {
	kinds := []string{
		"TRANSFER_RECONCILIATION", "WEALTH_OBSERVATION", "TRANSFER_CLASSIFICATION",
		"UNKNOWN_MERCHANT", "AMBIGUOUS_CATEGORY", "CYCLE_RESIDUAL_ALLOCATION",
		"PAYSLIP_CONFIRMATION", "SALARY_SOURCE_CONFIRMATION", "MISSING_PAY_DATE",
		"UNKNOWN_BANK_TEMPLATE", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE",
		"DOCUMENT_CLASSIFICATION", "POSSIBLE_DUPLICATE", "FINANCIAL_EMAIL_RESOLUTION",
		"FINANCIAL_EMAIL_FACTS", "CONFLICTING_EVIDENCE", "UNKNOWN_PURPOSE",
		"MANUAL_CORRECTION", "RECEIPT_MISMATCH", "MISSING_TRANSACTION_DATE",
		"TRANSACTION_FACTS_MISSING", "MISSING_AMOUNT", "UNKNOWN_EMAIL_TEMPLATE",
		"INVOICE_PAYMENT_STATUS", "NOT_A_KIND",
	}
	out := map[string][]string{}
	for _, kind := range kinds {
		out[kind] = reviewdec.AgentActions(kind)
	}
	got, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/agent_review_actions.golden.json"
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
		t.Fatalf("agent review actions changed; review the diff and rerun with -update if intended\n%s", got)
	}
}
