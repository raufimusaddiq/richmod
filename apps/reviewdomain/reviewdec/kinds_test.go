package reviewdec

import (
	"slices"
	"testing"
)

func TestAllAgentActionsKeepsClosedVocabulary(t *testing.T) {
	want := []string{"CONFIRM", "IGNORE", "PRIMARY_SALARY", "ORDINARY_INCOME", "SET_TRANSACTION_DATE", "COMPLETE_BANK_FACTS", "REPROCESS_DOCUMENT", "SET_FINANCIAL_EMAIL_ENTITIES", "MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE"}
	got := AllAgentActions()
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("agent action vocabulary changed:\n got  %v\n want %v", got, want)
	}
}

func TestKindReasonsAreUniqueAndLookupsDoNotAlias(t *testing.T) {
	seen := map[string]bool{}
	for _, kind := range kinds {
		if seen[kind.Reason] {
			t.Fatalf("duplicate kind %s", kind.Reason)
		}
		seen[kind.Reason] = true
	}
	AgentActions("POSSIBLE_DUPLICATE")[0] = "X"
	InboxActions("PAYSLIP_CONFIRMATION")[0] = "X"
	if AgentActions("POSSIBLE_DUPLICATE")[0] != "MERGE_EXISTING" || InboxActions("PAYSLIP_CONFIRMATION")[0] != "PRIMARY_SALARY" {
		t.Fatal("lookup returned a slice aliasing the kind table")
	}
	decision, _ := Preset("POSSIBLE_DUPLICATE", "transaction", "t")
	decision.AllowedActions[0] = "X"
	if again, _ := Preset("POSSIBLE_DUPLICATE", "transaction", "t"); again.AllowedActions[0] != "MERGE_EXISTING" {
		t.Fatal("Preset returned a slice aliasing the kind table")
	}
}
