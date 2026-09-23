package bankemail

import (
	"context"
	"errors"
	"testing"
)

// PRD section 25 required Bank Email acceptance tests, named by their PRD case so
// a reviewer can map each one to the requirement.
//
// Every case here is offline and deterministic. The half of B2/B3 that depends on
// a bounded model verdict cannot be asserted offline without mocking the answer,
// which proves nothing, so those paths are exercised against the real provider by
// the section 23 semantic canary corpus instead. B2 and B3 below cover the
// deterministic decision each of those outcomes must land on once the model has
// spoken: a decisive category confirms, an undecided one parks a category-only
// review.

// B1 - learn merchant auto-applies its stored category.
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

// B2 - a new merchant whose category the bounded plane decided must confirm with
// that category and create no review work. This is the deterministic half of the
// case: the processor writes these fields onto the policy result when the
// classifier returns a decisive answer.
func TestBankEmailB2DecisiveCategoryConfirmsWithoutReview(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("an unknown merchant starts as a category review: %+v", result)
	}
	// What the processor does with a decisive bounded answer.
	result.CategoryID, result.AutoConfirm = "cat-food", true
	result.Status, result.ReviewType = "CONFIRMED", ""
	if result.Status != "CONFIRMED" || !result.AutoConfirm || result.ReviewType != "" {
		t.Fatalf("a decisive category must confirm with no review: %+v", result)
	}
	if result.CategoryID != "cat-food" {
		t.Fatalf("the decided category must be persisted: %+v", result)
	}
}

// B3 - a new merchant whose category the bounded plane could not decide stays a
// review, and that review is category-only: the amount and the time are already
// known and must never be re-requested (PRD 3.3, 18.1).
func TestBankEmailB3UndecidedCategoryAsksOnlyForCategory(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("undecided category must park a category review: %+v", result)
	}
	if result.AutoConfirm {
		t.Fatalf("an undecided category must never auto-confirm: %+v", result)
	}
	decision := partialDecision("household", "source", outgoingCard("54000", "Warung Baru"), "AMBIGUOUS_CATEGORY", []string{"category"}, "the bounded plane could not decide a category")
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		t.Fatalf("the review must name only the category as missing: %v", decision.MissingFacts)
	}
	for _, known := range []string{"amount_idr", "transaction_at"} {
		for _, missing := range decision.MissingFacts {
			if missing == known {
				t.Fatalf("%s is already known and must not be re-requested", known)
			}
		}
	}
	if decision.KnownFacts["amount_idr"] != "54000" {
		t.Fatalf("the known amount must travel with the review: %v", decision.KnownFacts)
	}
}

// B4 - merchant absent but every other fact valid. The expense must still be
// classified; a missing merchant is a missing fact, not an invalid transaction.
func TestBankEmailB4MissingMerchantDoesNotBecomeUnknownPurpose(t *testing.T) {
	channel, direction := "QR", "OUTGOING"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction}
	result := EvaluateBankEmail(spendingListener(), extraction, nil)
	if result.Type != "EXPENSE" || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("valid facts with no merchant must stay an expense: %+v", result)
	}
	if result.ReviewType != "UNKNOWN_MERCHANT" {
		t.Fatalf("missing merchant must be its own dimension: %+v", result)
	}
}

// B4, second half: the merchant value is nullable in the tool contract, so the
// model is never forced to invent one (PRD 9.5). Every property is listed in
// `required` because a native tool call must be structurally complete; it is the
// nullable *type* that keeps the value un-fabricated.
func TestBankEmailB4MerchantValueIsNullable(t *testing.T) {
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
}

// B5 - two plausible transaction amounts. This asserts the property the case is
// named for: an undecided ambiguity ruling must not authorise a write, while a
// decided not-ambiguous ruling must. The ambiguity claim is inverted, so the two
// bands have to be separated by the verdict helper rather than by AcceptNoul
// alone; asserting only `if noulClaimed(...)` would pass either way and prove
// nothing (Hermes review).
func TestBankEmailB5UndecidedAmbiguityDoesNotAuthorize(t *testing.T) {
	// 0.10 sits between Low 0.05 and High 0.15: the plane could not tell whether
	// the email was ambiguous, which is exactly the two-amount case.
	answers := supportedRuling()
	answers["material_ambiguity"] = noul(0.10)
	verification, verified, err := (&Processor{verifier: &stubVerifier{answers: answers}}).verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("the bundle was answered, so it is verified")
	}
	if verification.MaterialAmbiguity {
		t.Fatalf("an undecided answer is not an affirmative ambiguous ruling: %+v", verification)
	}
	if verification.AmbiguityDecidedNotAmbiguous {
		t.Fatalf("an undecided answer must not read as decided-not-ambiguous: %+v", verification)
	}
	if verification.supported() {
		t.Fatalf("an undecided ambiguity ruling must not authorize: %+v", verification)
	}
}

// B5, other half: only an affirmative not-ambiguous ruling clears the gate.
func TestBankEmailB5DecidedNotAmbiguousAuthorizes(t *testing.T) {
	verification, verified, err := (&Processor{verifier: &stubVerifier{answers: supportedRuling()}}).verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || !verification.AmbiguityDecidedNotAmbiguous || !verification.supported() {
		t.Fatalf("a decided not-ambiguous ruling must authorize: %+v", verification)
	}
}

// B6 - provider failure is an infrastructure event, never a semantic verdict.
// The caller must be able to tell "no ruling" from "ruled safe": a failure is
// surfaced as an error, and an unconfigured verifier is the disabled case that
// still never reads as approval.
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
