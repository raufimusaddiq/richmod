package telegram

import "testing"

func TestReviewInitialStateAsksForOnlyRequiredFact(t *testing.T) {
	tests := []struct {
		name        string
		reviewType  string
		wantState   string
		wantMessage string
		context     string
		wantMode    string
	}{
		{"unknown merchant", "UNKNOWN_MERCHANT", "AWAITING_CATEGORY", "Nominal: Rp18.502\nWaktu: 02/09/2026 09:24 WIB", "Nominal: Rp18.502\nWaktu: 02/09/2026 09:24 WIB", "category"},
		{"missing purpose", "UNKNOWN_PURPOSE", "AWAITING_DETAIL", "🟡 Perlu detail transaksi\n\nNominal: Rp18.502\n\nBalas pesan ini dengan keterangan atau tujuan transaksi.", "Nominal: Rp18.502", "reply"},
		{"missing category", "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", "keep context", "keep context", "category"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, message, mode := reviewInitialState(tt.reviewType, tt.context)
			if state != tt.wantState || message != tt.wantMessage || mode != tt.wantMode {
				t.Fatalf("state=%q message=%q mode=%q", state, message, mode)
			}
		})
	}
}

func TestRequiredFieldReplyMarkupOnlyOffersIgnore(t *testing.T) {
	markup := requiredFieldReplyMarkup()
	if len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 || markup.InlineKeyboard[0][0].CallbackData != "review:ignore" {
		t.Fatalf("markup=%#v", markup)
	}
}

func TestReviewRequiresFactPreservesLegacyAndCategoryOnlyContract(t *testing.T) {
	for _, raw := range []*string{nil, reviewFactPtr("null"), reviewFactPtr("invalid")} {
		if !reviewRequiresFact(raw, "merchant") {
			t.Fatalf("missing/malformed contract must require legacy merchant fact: %v", raw)
		}
	}
	facts := reviewFactPtr(`["category"]`)
	if reviewRequiresFact(facts, "merchant") || !reviewRequiresFact(facts, "category") {
		t.Fatal("category-only contract should not require merchant, and must require category")
	}
}

func TestCategorySelectionAllowsMissingMerchantWhenContractIsCategoryOnly(t *testing.T) {
	categoryOnly := reviewFactPtr(`["category"]`)
	if reviewRequiresFact(categoryOnly, "merchant") || !reviewRequiresFact(categoryOnly, "category") {
		t.Fatal("category-only review must not block category selection on a missing merchant")
	}
}

func reviewFactPtr(value string) *string { return &value }
