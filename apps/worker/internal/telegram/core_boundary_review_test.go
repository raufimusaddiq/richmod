package telegram

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// The action vocabulary is keyed by semantic review type, not binding kind: a TRANSACTION binding with a
// TRANSFER_CLASSIFICATION review gets the transfer set.
func TestT1TransferClassificationReviewExposesSemanticActions(t *testing.T) {
	got := reviewdec.AgentActions("TRANSFER_CLASSIFICATION")
	want := []string{"EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "IGNORE"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("TRANSFER_CLASSIFICATION actions=%v", got)
	}
	if strings.Join(reviewdec.AgentActions("TRANSACTION"), ",") == strings.Join(want, ",") {
		t.Fatal("binding kind TRANSACTION must not alias the transfer review vocabulary")
	}
}

func TestT5ResolveReviewSchemaCarriesTypedTransferArgs(t *testing.T) {
	for _, tool := range NativeFinanceTools(nil, false, false, true, "TRANSFER_CLASSIFICATION", false, false, "TRANSACTION") {
		if tool.Name != "resolve_review" {
			continue
		}
		encoded, _ := json.Marshal(tool.Parameters)
		text := string(encoded)
		for _, want := range []string{"ASSET_PURCHASE", "wealth_account_hint", "EXPENSE", "category_slug", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER"} {
			if !strings.Contains(text, want) {
				t.Fatalf("schema missing %q: %s", want, text)
			}
		}
		if strings.Contains(text, "\"CONFIRM\"") {
			t.Fatalf("transfer review must not expose generic CONFIRM: %s", text)
		}
		return
	}
	t.Fatal("resolve_review missing")
}
