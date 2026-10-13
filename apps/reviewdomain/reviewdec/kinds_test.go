package reviewdec

import (
	"slices"
	"testing"
)

// The agent's closed action vocabulary, as it stood before the kind table
// became its source. Membership is what the validator checks, so order is free.
func TestAllAgentActionsKeepsClosedVocabulary(t *testing.T) {
	want := []string{"CONFIRM", "IGNORE", "EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "PRIMARY_SALARY", "ORDINARY_INCOME", "SET_PAY_DATE", "COMPLETE_BANK_FACTS", "REPROCESS_DOCUMENT", "SET_FINANCIAL_EMAIL_ENTITIES", "ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING", "MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE"}
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
	// A caller mutating a returned slice must not corrupt the table.
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
