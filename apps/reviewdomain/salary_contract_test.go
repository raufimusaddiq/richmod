package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

func TestTelegramConfirmLanesUseSharedSalaryRecording(t *testing.T) {
	for _, path := range []string{
		telegramReviewSourceGlob,
		"../worker/internal/telegram/agent_review_mutations.go",
	} {
		text := string(mustReadPaths(t, path))
		if !strings.Contains(text, "reviewdomain.RecordSalaryEvent") && !strings.Contains(text, "RecordSalaryEvent(") {
			t.Fatalf("%s does not call the shared salary operation", path)
		}
		if strings.Contains(text, "INSERT INTO salary_event") || strings.Contains(text, "INSERT INTO salary_source") {
			t.Fatalf("%s still owns salary mutation SQL", path)
		}
	}
}

func TestWebPayslipResolutionUsesSharedProposalTransition(t *testing.T) {
	source, err := os.ReadFile("../api/internal/review/canonical.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "reviewdomain.ResolvePayslipProposal(") {
		t.Fatal("Web payslip resolution bypasses the shared proposal transition")
	}
	if strings.Contains(text, "INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,counterparty_name") {
		t.Fatal("Web still owns payslip proposal-to-transaction mutation SQL")
	}
}

func TestDocumentPromotionIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		telegramReviewSourceGlob,
		"../worker/internal/telegram/agent_review_mutations.go",
	} {
		text := string(mustReadPaths(t, path))
		if !strings.Contains(text, "PromoteEvidenceDocuments") {
			t.Fatalf("%s does not call the shared document promotion", path)
		}
		if strings.Contains(text, "UPDATE document d SET status='EXTRACTED'") {
			t.Fatalf("%s still owns document promotion SQL", path)
		}
	}
}
