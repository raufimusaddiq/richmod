package telegram

import "testing"

func TestReviewInitialStateCollectsMissingFactsBeforeCategory(t *testing.T) {
	tests := []struct {
		name        string
		reviewType  string
		wantState   string
		wantMessage string
		context     string
		wantMode    string
	}{
		{"missing merchant", "UNKNOWN_MERCHANT", "AWAITING_CATEGORY", "keep context", "keep context", "category"},
		{"missing purpose", "UNKNOWN_PURPOSE", "AWAITING_DETAIL", "🟡 Perlu detail transaksi\n\nNominal: Rp18.502\n\nBalas pesan ini dengan keterangan atau tujuan transaksi.", "Nominal: Rp18.502", "reply"},
		{"possible duplicate", "POSSIBLE_DUPLICATE", "AWAITING_DETAIL", "Transaksi ini mungkin duplikat. Selesaikan melalui Review Inbox untuk memilih gabung atau catat baru.", "candidate exists", "reply"},
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
