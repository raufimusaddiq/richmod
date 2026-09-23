package bankemail

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// PRD §33: the bank-email category auto-confirm path has an independent
// kill-switch. When off, a remembered merchant category must not confirm ledger
// money; it parks a category-carrying review instead.
func TestCategoryAutoConfirmKillSwitchParksRememberedMerchant(t *testing.T) {
	channel, direction, merchant := "DEBIT_CARD", "OUTGOING", "Toko Contoh"
	extraction := Extraction{
		Kind: "TRANSACTION", AmountIDR: ptr("54000"), TransactionAt: timePtr(),
		Channel: &channel, Direction: &direction, Merchant: &merchant,
	}
	policy := EvaluateBankEmail(Listener{TrackingPolicy: "SPENDING_ONLY"}, extraction, nil, MerchantMemory{CategoryID: "cat-food", AutoApply: true})
	if !policy.AutoConfirm || policy.Status != "CONFIRMED" {
		t.Fatalf("default policy should auto-confirm a remembered merchant: %+v", policy)
	}

	kept := applyCategoryAutoConfirmSwitch(policy, true)
	if !kept.AutoConfirm || kept.Status != "CONFIRMED" {
		t.Fatalf("enabled switch must not change the policy result: %+v", kept)
	}

	off := applyCategoryAutoConfirmSwitch(policy, false)
	if off.AutoConfirm || off.Status != "NEEDS_REVIEW" || off.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("disabled switch must park a review: %+v", off)
	}
	if off.CategoryID != "cat-food" {
		t.Fatalf("the decided category must still ride on the review: %+v", off)
	}
}

// A known-account transfer auto-confirms too, but it has no category. Gating it
// would park a transfer as a category review with an empty picker.
func TestCategoryAutoConfirmKillSwitchLeavesTransfersAlone(t *testing.T) {
	transfer := PolicyResult{Type: "TRANSFER", Status: "CONFIRMED", AutoConfirm: true, Description: "Transfer antar rekening yang dikenal."}
	kept := applyCategoryAutoConfirmSwitch(transfer, false)
	if !kept.AutoConfirm || kept.Status != "CONFIRMED" {
		t.Fatalf("a categoriless transfer must not be parked as a category review: %+v", kept)
	}
}

// A review that was never auto-confirmable is untouched by the switch.
func TestCategoryAutoConfirmKillSwitchLeavesReviewsAlone(t *testing.T) {
	review := PolicyResult{Type: "EXPENSE", Status: "NEEDS_REVIEW", ReviewType: "UNKNOWN_MERCHANT"}
	if got := applyCategoryAutoConfirmSwitch(review, false); got != review {
		t.Fatalf("switch altered an already-parked review: %+v", got)
	}
}

// With the switch off the bounded category path must not run either: it also
// confirms ledger money, so a switch that gated only the remembered-merchant
// branch would be re-opened by the classifier it exists to stop.
func TestCategoryAutoConfirmKillSwitchGatesTheBoundedPath(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{answers: map[string]judgment.Answer{"category": {
		Distribution:  map[string]float64{"food-and-drink": 0.9, "groceries": 0.1, "OTHER_OR_UNCLEAR": 0},
		Confidence:    0.9,
		HasConfidence: true,
	}}}}
	processor.SetCategoryAutoConfirm(false)
	if processor.categoryAutoConfirm {
		t.Fatal("the switch must be off")
	}
	policy := PolicyResult{Type: "EXPENSE", Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY"}
	gated := applyCategoryAutoConfirmSwitch(policy, processor.categoryAutoConfirm)
	if gated.AutoConfirm {
		t.Fatalf("a disabled switch must not auto-confirm: %+v", gated)
	}
}
