package telegram

import "testing"

func TestReviewInitialStateCollectsMissingFactsBeforeCategory(t *testing.T) {
	tests := []struct {
		name        string
		reviewType  string
		wantState   string
		wantMessage string
		wantMode    string
	}{
		{"missing merchant", "UNKNOWN_MERCHANT", "AWAITING_MERCHANT", "Nama merchant belum tersedia. Balas pesan ini dengan nama merchant.", "detail"},
		{"missing purpose", "UNKNOWN_PURPOSE", "AWAITING_DETAIL", "Keterangan transaksi belum tersedia. Balas pesan ini dengan detail transaksi.", "detail"},
		{"missing category", "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", "keep context", "category"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, message, mode := reviewInitialState(tt.reviewType, tt.wantMessage)
			if state != tt.wantState || message != tt.wantMessage || mode != tt.wantMode {
				t.Fatalf("state=%q message=%q mode=%q", state, message, mode)
			}
		})
	}
}
