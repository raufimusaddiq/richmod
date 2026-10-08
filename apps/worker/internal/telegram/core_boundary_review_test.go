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

// T6 and T15: Jev may fast-finish only finite, argument-free actions. Actions
// that need arbitrary facts present in the turn must fall through to the typed
// generative tool; there is no Go phrase parser deciding this.
func TestT6ArgumentBearingActionsDeferToGenerativeExtraction(t *testing.T) {
	for _, action := range []string{"EXPENSE", "SET_PAY_DATE", "COMPLETE_BANK_FACTS"} {
		if !isReviewAction("TRANSFER_CLASSIFICATION", action) && action != "SET_PAY_DATE" && action != "COMPLETE_BANK_FACTS" {
			t.Fatalf("%s must be an allowed transfer review action", action)
		}
		if !reviewActionNeedsArguments(action) {
			t.Fatalf("%s must defer to generative extraction for its arbitrary args", action)
		}
	}
	if !isReviewAction("TRANSFER_CLASSIFICATION", "ASSET_PURCHASE") || !reviewActionNeedsArguments("ASSET_PURCHASE") {
		t.Fatal("ASSET_PURCHASE is a bounded transfer action needing generative argument extraction")
	}
	for _, action := range []string{"IGNORE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER"} {
		if reviewActionNeedsArguments(action) {
			t.Fatalf("%s is argument-free and may finish via Jev", action)
		}
	}
}

// T6b: after Jev decides an argument-bearing action, the generative tool is
// pinned to that single action so the freeform value is extracted rather than
// the meaning being re-decided.
func TestNarrowReviewActionToolPinsJevChoice(t *testing.T) {
	tools := NativeFinanceTools(nil, false, false, true, "TRANSFER_CLASSIFICATION", false, false, "TRANSACTION")
	narrowReviewActionTool(tools, "ASSET_PURCHASE")
	var found bool
	for _, tool := range tools {
		if tool.Name != "resolve_review" {
			continue
		}
		found = true
		properties := tool.Parameters["properties"].(map[string]any)
		actionSchema := properties["action"].(map[string]any)
		if got := actionSchema["enum"].([]string); len(got) != 1 || got[0] != "ASSET_PURCHASE" {
			t.Fatalf("enum=%v; want only ASSET_PURCHASE", got)
		}
	}
	if !found {
		t.Fatal("resolve_review missing")
	}
}
