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
		{"missing merchant", "UNKNOWN_MERCHANT", "AWAITING_MERCHANT", "🟡 Perlu detail merchant\n\nBalas pesan ini dengan nama merchant untuk transaksi tersebut.", "detail"},
		{"missing purpose", "UNKNOWN_PURPOSE", "AWAITING_DETAIL", "🟡 Perlu detail transaksi\n\nBalas pesan ini dengan keterangan atau tujuan transaksi.", "detail"},
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
