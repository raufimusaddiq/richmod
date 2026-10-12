package telegram

import (
	"encoding/json"
	"strings"
	"testing"
)

// T1: an exact TRANSACTION binding with a TRANSFER_CLASSIFICATION review type
// must expose the transfer-classification action vocabulary, NOT the generic
// TRANSACTION default. The schema is keyed by semantic review type, and the
// binding kind is a separate axis.
func TestT1TransferClassificationReviewExposesSemanticActions(t *testing.T) {
	got := reviewActionsForType("TRANSFER_CLASSIFICATION")
	want := []string{"EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "IGNORE"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("TRANSFER_CLASSIFICATION actions=%v", got)
	}
	// The binding kind, if it had leaked in as the semantic key, would fall to the
	// generic default. Prove the two are not interchangeable.
	if strings.Join(reviewActionsForType("TRANSACTION"), ",") == strings.Join(want, ",") {
		t.Fatal("binding kind TRANSACTION must not alias the transfer review vocabulary")
	}
}

// T5: the resolve_review schema exposes the exact transfer vocabulary and the
// typed ASSET_PURCHASE argument surface, so a generative call can carry
// wealth_account_hint in one turn.
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
