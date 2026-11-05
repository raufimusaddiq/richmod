package reviewdomain

import (
	"slices"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestExecutableActionsFollowTheStoredDecisionAndTheBinding(t *testing.T) {
	bankSource := Binding{Subject: SubjectSourceEvent, SourceType: "BANK_EMAIL"}
	for _, tt := range []struct {
		name, kind   string
		stored, want []string
		binding      Binding
		notice       bool
	}{
		{"bank email low confidence offers bank facts, never reprocess", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", []string{"COMPLETE_BANK_FACTS", "IGNORE"}, []string{"COMPLETE_BANK_FACTS", "IGNORE"}, bankSource, false},
		{"bank email low confidence legacy row drops reprocess", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", nil, []string{"IGNORE"}, bankSource, true},
		{"document low confidence reprocesses", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", []string{"REPROCESS_DOCUMENT", "IGNORE"}, []string{"REPROCESS_DOCUMENT", "IGNORE"}, Binding{Subject: SubjectDocument, HasDocument: true}, false},
		{"provider facts are a notice", "FINANCIAL_EMAIL_FACTS", []string{"IGNORE"}, []string{"IGNORE"}, Binding{Subject: SubjectFinancialEmail}, true},
		{"orphaned duplicate is a notice", "POSSIBLE_DUPLICATE", []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}, []string{"IGNORE"}, Binding{Subject: SubjectTransaction, TransactionStatus: "CONFIRMED"}, true},
		{"open duplicate keeps its stored answers", "POSSIBLE_DUPLICATE", []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}, []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}, Binding{Subject: SubjectTransaction, TransactionStatus: "NEEDS_REVIEW"}, false},
		{"unknown merchant open uses stored decision", "UNKNOWN_MERCHANT", []string{"CONFIRM_REVIEW", "IGNORE"}, []string{"CONFIRM_REVIEW", "IGNORE"}, Binding{Subject: SubjectTransaction, TransactionStatus: "NEEDS_REVIEW"}, false},
		{"transfer case without candidates offers a new transfer", "TRANSFER_CLASSIFICATION", []string{"CLASSIFY_TRANSFER", "IGNORE"}, []string{"CONFIRM_NEW_TRANSFER", "IGNORE"}, Binding{Subject: SubjectSourceEvent, HasTransferCase: true}, false},
		{"overflowing financial transfer merges among the ranked set only", "TRANSFER_CLASSIFICATION", []string{"IGNORE"}, []string{"MERGE_EXISTING", "IGNORE"}, Binding{Subject: SubjectFinancialEmail, HasTransferCase: true, TransferCandidates: 14}, false},
		{"transfer without a case can only be dismissed", "TRANSFER_CLASSIFICATION", []string{"CLASSIFY_TRANSFER", "IGNORE"}, []string{"IGNORE"}, Binding{Subject: SubjectSourceEvent}, true},
		{"legacy transaction date action still answers", "MISSING_TRANSACTION_DATE", []string{"SET_PAY_DATE", "IGNORE"}, []string{"SET_PAY_DATE", "IGNORE"}, Binding{Subject: SubjectTransaction, TransactionStatus: "NEEDS_REVIEW"}, false},
		{"residual is never silently ignored", "CYCLE_RESIDUAL_ALLOCATION", nil, []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"}, Binding{Subject: SubjectCycleResidual}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ExecutableActions(tt.kind, tt.stored, tt.binding)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("actions=%v want %v", got, tt.want)
			}
			if (PresentationFor(got) == PresentationNotice) != tt.notice {
				t.Fatalf("presentation=%s notice=%v", PresentationFor(got), tt.notice)
			}
		})
	}
}

// Every action a surface can be offered must be executable for that binding;
// the filter is the only producer of offered actions, so this pins that no
// preset action survives on a binding whose resolver would refuse it.
func TestNoOfferedActionIsUnexecutable(t *testing.T) {
	bindings := []Binding{
		{Subject: SubjectTransaction, TransactionStatus: "NEEDS_REVIEW"},
		{Subject: SubjectTransaction, TransactionStatus: "CONFIRMED"},
		{Subject: SubjectFinancialEmail, HasTransferCase: true, TransferCandidates: 3},
		{Subject: SubjectFinancialEmail},
		{Subject: SubjectProposal},
		{Subject: SubjectSourceEvent, SourceType: "BANK_EMAIL"},
		{Subject: SubjectSourceEvent, SourceType: "TELEGRAM_TEXT", HasTransferCase: true},
		{Subject: SubjectDocument, HasDocument: true},
		{Subject: SubjectWealthObservation},
		{Subject: SubjectCycleResidual},
	}
	for _, kind := range reviewdec.ActiveReasons() {
		preset, _ := reviewdec.Preset(kind, "test", "test")
		for _, b := range bindings {
			for _, stored := range [][]string{nil, preset.AllowedActions} {
				for _, action := range ExecutableActions(kind, stored, b) {
					if !Executable(kind, action, b) {
						t.Fatalf("%s on %+v offers unexecutable %s", kind, b, action)
					}
				}
			}
		}
	}
}

func TestTransactionDateChoicesAreBoundedJakartaDates(t *testing.T) {
	now := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC) // 01:00 WIB on 6 Nov
	message := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	got := TransactionDateChoices(message, true, now)
	want := []DateChoice{{DateChoiceToday, "Hari ini", "2026-11-06"}, {DateChoiceYesterday, "Kemarin", "2026-11-05"}, {DateChoiceMessageDate, "Tanggal pesan", "2026-11-02"}}
	if !slices.Equal(got, want) {
		t.Fatalf("choices=%v", got)
	}
	if got := TransactionDateChoices(now, true, now); len(got) != 2 {
		t.Fatalf("a message date equal to today is not repeated: %v", got)
	}
	if got := TransactionDateChoices(message, false, now); len(got) != 2 {
		t.Fatalf("a printed (non-fallback) time offers no message date: %v", got)
	}
}

func TestLedgerMatchingIsDeterministic(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"Alfamart", "QRIS ALFAMART SUDIRMAN", true},
		{"Indomaret", "indomaret", true},
		{"Kopi", "Kopi Kenangan", false}, // too short to prove anything
		{"Alfamart", "Indomaret", false},
		{"", "Alfamart", false},
	} {
		if got := CompatibleMerchant(tt.a, tt.b); got != tt.want {
			t.Fatalf("CompatibleMerchant(%q,%q)=%v", tt.a, tt.b, got)
		}
	}
	strong := LedgerMatch{ID: "a", Score: LedgerMatchScore(0.5, true)}
	weak := LedgerMatch{ID: "b", Score: LedgerMatchScore(0.5, false)}
	twin := LedgerMatch{ID: "d", Score: LedgerMatchScore(3, true)}
	if got := StrongLedgerMatch([]LedgerMatch{strong, twin}); got != nil {
		t.Fatalf("two compatible same-day matches (%.2f) are ambiguous", twin.Score)
	}
	if got := StrongLedgerMatch([]LedgerMatch{strong, weak}); got == nil {
		t.Fatalf("a merchant-less runner-up (%.2f) does not block a unique strong match", weak.Score)
	}
	far := LedgerMatch{ID: "c", Score: LedgerMatchScore(30, true)}
	if got := StrongLedgerMatch([]LedgerMatch{strong, far}); got == nil || got.ID != "a" {
		t.Fatalf("unique strong match must link: %v", got)
	}
	if got := StrongLedgerMatch([]LedgerMatch{weak}); got != nil {
		t.Fatal("a merchant-less match never links")
	}
}
