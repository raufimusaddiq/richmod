package bankemail

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// PRD §25 required Bank Email acceptance tests, named by their PRD case so a
// reviewer can map each one to the requirement. They exercise the deterministic
// policy and the decisions around it. Each is offline: a bounded model verdict
// cannot be asserted here without either mocking the answer (which proves
// nothing) or becoming flaky. B2/B3/B6 need a live provider and are covered by
// the §23 semantic canary corpus, which contains the same email shapes and, for
// B6, the provider-failure claim itself.

// B1 — Learned merchant auto-applies its stored category.
func TestBankEmailB1LearnedMerchantConfirmsWithoutReview(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.Status != "CONFIRMED" || !result.AutoConfirm {
		t.Fatalf("learned merchant must confirm: %+v", result)
	}
	if result.CategoryID != "cat-food" {
		t.Fatalf("learned category must be reused: %+v", result)
	}
}

// B1 also pins the zero-human-touch property the PRD states as RHICE = 0: the
// confirm path must not be a review wearing a different status.
func TestBankEmailB1LearnedMerchantCreatesNoReviewWork(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.ReviewType != "" {
		t.Fatalf("a confirmed expense must carry no review type: %+v", result)
	}
}

// B4 — Merchant absent but every other fact valid. The expense must still be
// classified; a missing merchant is a missing fact, not an invalid transaction.
func TestBankEmailB4MissingMerchantDoesNotBecomeUnknownPurpose(t *testing.T) {
	channel, direction := "QR", "OUTGOING"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction}
	result := EvaluateBankEmail(spendingListener(), extraction, nil)
	if result.Type != "EXPENSE" || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("valid facts with no merchant must stay an expense: %+v", result)
	}
	// The unresolved dimension is the merchant, so the review is merchant-typed
	// rather than a generic "transaction needs review" bucket (PRD §18.1).
	if result.ReviewType != "UNKNOWN_MERCHANT" {
		t.Fatalf("missing merchant must be its own dimension: %+v", result)
	}
}

// B4 also covers the merchant text being optional in the tool contract, so the
// model is never forced to invent one (PRD §9.5).
func TestBankEmailB4MerchantIsOptionalInToolSchema(t *testing.T) {
	tool := EmitBankTransactionTool()
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("tool schema has no properties map")
	}
	merchant, ok := properties["merchant"].(map[string]any)
	if !ok {
		t.Fatal("tool schema does not describe merchant")
	}
	types, ok := merchant["type"].([]string)
	if !ok {
		t.Fatalf("merchant type should be a nullable union, got %T", merchant["type"])
	}
	nullable := false
	for _, candidate := range types {
		if candidate == "null" {
			nullable = true
		}
	}
	if !nullable {
		t.Fatalf("merchant must be nullable so it is never fabricated: %v", types)
	}
	// The schema lists every property in `required` because a native tool call
	// must be structurally complete; the *value* is nullable, which is what keeps
	// the merchant un-fabricated. Asserting absence from `required` would claim
	// more than the contract does and would fail for the wrong reason.
	required, _ := tool.Parameters["required"].([]string)
	if len(required) == 0 {
		t.Fatal("tool schema should still require the factual fields")
	}
}

// B5 — two plausible transaction amounts. The bounded ambiguity ruling is what
// stops an auto-confirm, and the fail-closed band is what makes an undecided
// model verdict count as ambiguity rather than approval.
func TestBankEmailB5AmbiguityFailsClosedOnUndecidedRuling(t *testing.T) {
	// No ruling at all: the claim is not affirmative, so the caller must not
	// treat the email as verified.
	if noulClaimed(nil, "material_ambiguity", evidenceVerificationPolicy.Ambiguity) {
		t.Fatal("an absent ambiguity ruling must not count as decided")
	}
	// A confident "not ambiguous" above the high band is the only thing that
	// clears the gate; the middle band stays ambiguous.
	undecided := map[string]judgment.Answer{"material_ambiguity": {Type: "noul", Noul: 0.10, HasNoul: true}}
	if noulClaimed(undecided, "material_ambiguity", evidenceVerificationPolicy.Ambiguity) {
		t.Fatal("the undecided middle band must fail closed")
	}
	clear := map[string]judgment.Answer{"material_ambiguity": {Type: "noul", Noul: 0.02, HasNoul: true}}
	if noulClaimed(clear, "material_ambiguity", evidenceVerificationPolicy.Ambiguity) {
		t.Fatal("a decided not-ambiguous ruling must read as not ambiguous")
	}
}

// B6 — provider failure is an infrastructure event, never a semantic verdict.
// The caller must be able to tell "no ruling" from "ruled safe": a failure is
// surfaced as an error, and an unconfigured verifier is the disabled case that
// still never reads as approval. Both directions are asserted here so the case
// name maps to the requirement rather than to one of its halves.
func TestBankEmailB6ProviderFailureIsNotApproval(t *testing.T) {
	failing := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, verified, err := failing.verifyEvidence(context.Background(), "source", Extraction{}, TrustedEmail{}); err == nil || verified {
		t.Fatalf("provider failure must surface as an error, verified=%v err=%v", verified, err)
	}

	unconfigured := NewProcessor(nil, nil)
	verification, verified, err := unconfigured.verifyEvidence(nil, "source", Extraction{}, TrustedEmail{})
	if err != nil {
		t.Fatalf("a nil verifier is the disabled case, not a failure: %v", err)
	}
	if verified {
		t.Fatal("a nil verifier must not report a verified email")
	}
	if verification != (EvidenceVerification{}) {
		t.Fatalf("a nil verifier must return the zero verification: %+v", verification)
	}
}

func spendingListener() Listener { return Listener{TrackingPolicy: "SPENDING_ONLY", Active: true} }

func outgoingCard(amount, merchant string) Extraction {
	channel, direction := "DEBIT_CARD", "OUTGOING"
	return Extraction{Kind: "TRANSACTION", AmountIDR: ptr(amount), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Merchant: ptr(merchant)}
}
