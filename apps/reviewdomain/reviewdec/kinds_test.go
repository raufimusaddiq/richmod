package reviewdec

import "testing"

func TestKindReasonsAreUniqueAndLookupsDoNotAlias(t *testing.T) {
	seen := map[string]bool{}
	for _, kind := range kinds {
		if seen[kind.Reason] {
			t.Fatalf("duplicate kind %s", kind.Reason)
		}
		seen[kind.Reason] = true
	}
	InboxActions("PAYSLIP_CONFIRMATION")[0] = "X"
	if InboxActions("PAYSLIP_CONFIRMATION")[0] != "PRIMARY_SALARY" {
		t.Fatal("lookup returned a slice aliasing the kind table")
	}
	decision, _ := Preset("POSSIBLE_DUPLICATE", "transaction", "t")
	decision.AllowedActions[0] = "X"
	if again, _ := Preset("POSSIBLE_DUPLICATE", "transaction", "t"); again.AllowedActions[0] != "MERGE_EXISTING" {
		t.Fatal("Preset returned a slice aliasing the kind table")
	}
}
