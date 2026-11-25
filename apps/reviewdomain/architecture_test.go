package reviewdomain

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	apiReviewHandler = "../api/internal/review/handler.go"
	apiReviewCanon   = "../api/internal/review/canonical.go"
	telegramReview   = "../worker/internal/telegram/review*.go"
	agentAssetBuy    = "../worker/internal/telegram/agent_asset_purchase.go"
)

type adapterRule struct {
	name      string
	paths     []string
	section   [2]string
	callsAny  []string
	forbidden []string
}

func TestSurfacesDelegateFinancialMutationsToReviewdomain(t *testing.T) {
	rules := []adapterRule{
		{
			name:      "confirm",
			paths:     []string{apiReviewHandler, telegramReview},
			callsAny:  []string{"reviewdomain.ConfirmTransactionReview"},
			forbidden: []string{"UPDATE transaction SET status='CONFIRMED'"},
		},
		{
			name:      "duplicate merge",
			paths:     []string{apiReviewHandler, telegramReview},
			callsAny:  []string{"reviewdomain.MergeDuplicateReview"},
			forbidden: []string{"INSERT INTO reconciliation_merge (", "INSERT INTO reconciliation_merge(", "proposal_status='MERGED'"},
		},
		{
			name:      "reject api",
			paths:     []string{apiReviewHandler},
			section:   [2]string{"func (h *Handler) Reject(", "type mergeInput"},
			callsAny:  []string{"reviewdomain.RejectTransactionReview"},
			forbidden: []string{"SET status='CANCELLED'", "proposal_status='REJECTED'"},
		},
		{
			name:      "reject telegram",
			paths:     []string{telegramReview},
			section:   [2]string{"func rejectReviewTx(", "\nfunc "},
			callsAny:  []string{"reviewdomain.RejectTransactionReview"},
			forbidden: []string{"SET status='CANCELLED'", "proposal_status='REJECTED'"},
		},
		{
			name:      "transfer classification api",
			paths:     []string{apiReviewHandler},
			section:   [2]string{"func (h *Handler) ClassifyTransfer(", "\nfunc "},
			callsAny:  []string{"reviewdomain.ClassifyTransferReview"},
			forbidden: []string{"UPDATE transaction SET type=", "proposal_status="},
		},
		{
			name:      "transfer classification telegram",
			paths:     []string{"../worker/internal/telegram/interaction_transfer.go"},
			section:   [2]string{"func (p *Processor) classifyTransfer(", "\nfunc "},
			callsAny:  []string{"reviewdomain.ClassifyTransferReview"},
			forbidden: []string{"UPDATE transaction SET type=", "proposal_status="},
		},
		{
			name:      "transfer reconciliation",
			paths:     []string{apiReviewCanon, "../worker/internal/telegram/interaction_transfer.go"},
			callsAny:  []string{"reviewdomain.ReconcileTransfer"},
			forbidden: []string{"UPDATE transfer_reconciliation_case"},
		},
		{
			name:      "salary recording",
			paths:     []string{telegramReview, "../worker/internal/telegram/interaction_salary.go"},
			callsAny:  []string{"ResolvePayslipProposal("},
			forbidden: []string{"INSERT INTO salary_event", "INSERT INTO salary_source"},
		},
		{
			name:      "payslip finalizer",
			paths:     []string{"../worker/internal/document/payslip.go"},
			callsAny:  []string{"reviewdomain.FinalizePayslip("},
			forbidden: []string{"INSERT INTO salary_event", "INSERT INTO salary_source"},
		},
		{
			name:      "payslip finalizer owner",
			paths:     []string{"payslip.go"},
			callsAny:  []string{"RecordSalaryEvent("},
			forbidden: []string{"INSERT INTO salary_event", "INSERT INTO salary_source"},
		},
		{
			name:      "web payslip proposal",
			paths:     []string{apiReviewCanon},
			callsAny:  []string{"reviewdomain.ResolvePayslipProposal("},
			forbidden: []string{"INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,counterparty_name"},
		},
		{
			name:      "document promotion",
			paths:     []string{telegramReview, "../worker/internal/telegram/interaction_engine.go"},
			callsAny:  []string{"PromoteEvidenceDocuments"},
			forbidden: []string{"UPDATE document d SET status='EXTRACTED'"},
		},
		{
			name:      "financial email resolution",
			paths:     []string{apiReviewCanon},
			callsAny:  []string{"ResolveFinancialEmailReview"},
			forbidden: []string{"UPDATE financial_email_observation SET resolved_account_id", "INSERT INTO financial_entity_alias"},
		},
		{
			name:      "cycle residual",
			paths:     []string{apiReviewCanon, "../worker/internal/telegram/interaction_cycle.go"},
			callsAny:  []string{"ApplyCycleResidual"},
			forbidden: []string{"INSERT INTO cycle_residual_allocation", "UPDATE cycle_residual_case SET basis_income_idr"},
		},
		{
			name:      "wealth observation",
			paths:     []string{apiReviewCanon, agentAssetBuy},
			callsAny:  []string{"DismissWealthObservation", "ResolveWealthObservation"},
			forbidden: []string{"UPDATE wealth_observation SET status='DISMISSED'", "UPDATE wealth_observation SET resolved_wealth_account_id"},
		},
		{
			name:      "wealth reclassification",
			paths:     []string{agentAssetBuy},
			callsAny:  []string{"ReclassifyWealthEvidence"},
			forbidden: []string{"document_type='TRANSACTION_HISTORY_SCREENSHOT'"},
		},
		{
			name:      "failed source api",
			paths:     []string{"../api/internal/integrationaction/handler.go"},
			callsAny:  []string{"reviewdomain.IgnoreFailedSource"},
			forbidden: []string{"UPDATE source_event"},
		},
		{
			name:      "failed source telegram",
			paths:     []string{"../worker/internal/telegram/agent_terminal.go"},
			callsAny:  []string{"reviewdomain.RecordFailedSourceAction"},
			forbidden: []string{"INSERT INTO integration_action"},
		},
		{
			name:      "failed source bank email",
			paths:     []string{"../worker/internal/bankemail/processor.go"},
			callsAny:  []string{"reviewdomain.RecordFailedSourceAction"},
			forbidden: []string{"INSERT INTO integration_action"},
		},
	}
	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			text := readSources(t, rule.paths...)
			if rule.section[0] != "" {
				from := strings.Index(text, rule.section[0])
				if from < 0 {
					t.Fatalf("section %q not found in %v", rule.section[0], rule.paths)
				}
				to := strings.Index(text[from+len(rule.section[0]):], rule.section[1])
				if to < 0 {
					t.Fatalf("section end %q not found after %q", rule.section[1], rule.section[0])
				}
				text = text[from : from+len(rule.section[0])+to]
			}
			called := false
			for _, call := range rule.callsAny {
				called = called || strings.Contains(text, call)
			}
			if !called {
				t.Fatalf("%v must call one of %v", rule.paths, rule.callsAny)
			}
			for _, fragment := range rule.forbidden {
				if strings.Contains(text, fragment) {
					t.Fatalf("%v must not own mutation SQL %q; reviewdomain does", rule.paths, fragment)
				}
			}
		})
	}
}

func TestSourceLifecycleIsDerivedOnlyByTheSharedRule(t *testing.T) {
	inline := regexp.MustCompile(`processing_status\s*=\s*CASE\b`)
	parkedSettle := regexp.MustCompile("(?s)SET\\s+processing_status\\s*=\\s*'PROCESSED'[^`]*processing_status\\s*=\\s*'NEEDS_REVIEW'")
	setStatus := regexp.MustCompile(`SET\s+processing_status`)
	parkedReply := regexp.MustCompile(`finishWithoutTransaction\([^)]*"NEEDS_REVIEW"|SET\s+processing_status\s*=\s*'NEEDS_REVIEW'`)
	documentParked := regexp.MustCompile(`UPDATE document SET status='NEEDS_REVIEW'`)
	var files []string
	for _, pattern := range []string{"*.go", "../api/internal/*/*.go", "../worker/internal/*/*.go"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	scanned := 0
	for _, file := range files {
		base := filepath.Base(file)
		if base == "lifecycle.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		switch {
		case inline.Match(raw):
			t.Errorf("%s derives source_event.processing_status inline; call RefreshSourceLifecycle", file)
		case parkedSettle.Match(raw):
			t.Errorf("%s settles a NEEDS_REVIEW source directly; call RefreshSourceLifecycle", file)
		case base == "reopen.go" && setStatus.Match(raw):
			t.Errorf("%s sets processing_status before the shared refresh; let RefreshSourceLifecycle own it", file)
		case !strings.Contains(file, "worker") && documentParked.Match(raw):
			t.Errorf("%s sets document.status=NEEDS_REVIEW directly; let RefreshSourceLifecycle settle it", file)
		}
		if strings.Contains(filepath.ToSlash(file), "worker/internal/telegram/") && base != "agent_specialized_mutations.go" && parkedReply.Match(raw) {
			t.Errorf("%s parks a Telegram reply event as NEEDS_REVIEW; settle it PROCESSED and leave the review on its own item", file)
		}
	}
	if scanned < 100 {
		t.Fatalf("scanned only %d sources; the globs are not reaching the api and worker trees", scanned)
	}
}

func readSources(t *testing.T, patterns ...string) string {
	t.Helper()
	var body strings.Builder
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil || len(paths) == 0 {
			t.Fatalf("no sources match %q (err=%v)", pattern, err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			body.Write(raw)
			body.WriteByte('\n')
		}
	}
	return body.String()
}
