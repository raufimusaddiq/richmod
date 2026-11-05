package reviewdomain

import (
	"slices"
	"testing"
)

// The derived capability list must keep every action the hand-kept list named
// before the registry became its source; Admin coverage counts against it.
func TestTelegramCompleteActionsKeepsEveryRegisteredLane(t *testing.T) {
	got := TelegramCompleteActions()
	for _, action := range []string{
		"CONFIRM_REVIEW", "CLASSIFY_TRANSFER", "ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED",
		"TRANSACTION_MISSING", "PRIMARY_SALARY", "ORDINARY_INCOME", "SET_PAY_DATE",
		"COMPLETE_BANK_FACTS", "REPROCESS_DOCUMENT", "SET_FINANCIAL_EMAIL_ENTITIES",
		"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE",
		"OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "EXPENSE", "ASSET_PURCHASE",
		"SET_AMOUNT",
	} {
		if !slices.Contains(got, action) {
			t.Fatalf("Telegram capability lost %s: %v", action, got)
		}
	}
	if slices.Contains(got, "IGNORE") {
		t.Fatal("IGNORE is universal and must not be listed as a Telegram-only capability")
	}
}
