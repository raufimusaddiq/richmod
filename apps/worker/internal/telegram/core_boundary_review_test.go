package telegram

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestAssistantNeverClassifiesATransferCard(t *testing.T) {
	if got := reviewdec.AgentActions("TRANSFER_CLASSIFICATION"); strings.Join(got, ",") != "IGNORE" {
		t.Fatalf("TRANSFER_CLASSIFICATION actions=%v", got)
	}
	for _, tool := range NativeFinanceTools(nil, false, false, true, "TRANSFER_CLASSIFICATION", false, false, "TRANSACTION") {
		if tool.Name != "resolve_review" {
			continue
		}
		encoded, _ := json.Marshal(tool.Parameters)
		for _, banned := range []string{"ASSET_PURCHASE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER"} {
			if strings.Contains(string(encoded), banned) {
				t.Fatalf("the assistant can still classify a transfer with %s: %s", banned, encoded)
			}
		}
	}
}
