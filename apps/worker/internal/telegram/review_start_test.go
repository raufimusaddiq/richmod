package telegram

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// The renderer follows the stored decision, not the review type, so a date,
// policy, or duplicate review can never be rendered as a category chooser.
func TestRenderReviewPresentationFollowsDecision(t *testing.T) {
	tests := []struct {
		name        string
		reviewType  string
		decision    reviewdec.Decision
		wantState   string
		wantMessage string
		context     string
		wantMode    string
	}{
		{
			name: "category only is a bounded chooser", reviewType: "UNKNOWN_MERCHANT",
			decision:  reviewdec.Decision{DecisionClass: reviewdec.ClassEvidenceGap, MissingFacts: []string{"category"}, AllowedActions: []string{"CONFIRM_REVIEW", "IGNORE"}, InteractionMode: reviewdec.ModeBoundedChoice},
			wantState: "AWAITING_CATEGORY", wantMessage: "keep context", context: "keep context", wantMode: "category",
		},
		{
			name: "missing date is a bound reply", reviewType: "MISSING_TRANSACTION_DATE",
			decision:  reviewdec.Decision{MissingFacts: []string{"transaction_at"}, AllowedActions: []string{"SET_PAY_DATE", "IGNORE"}, InteractionMode: reviewdec.ModeSingleField},
			wantState: "AWAITING_DATE", wantMessage: "\U0001F7E1 Tanggal transaksi belum ada\n\nNominal: Rp18.502\n\nBalas pesan ini dengan tanggal transaksi (YYYY-MM-DD).", context: "Nominal: Rp18.502", wantMode: "reply",
		},
		{
			name: "compound residual collects the date first", reviewType: "TRANSACTION_FACTS_MISSING",
			decision:  reviewdec.Decision{MissingFacts: []string{"category", "transaction_at"}, AllowedActions: []string{"CONFIRM_REVIEW", "SET_PAY_DATE", "IGNORE"}, InteractionMode: reviewdec.ModeSingleField},
			wantState: "AWAITING_DATE", wantMessage: "\U0001F7E1 Tanggal transaksi belum ada\n\nNominal: Rp18.502\n\nBalas pesan ini dengan tanggal transaksi (YYYY-MM-DD).", context: "Nominal: Rp18.502", wantMode: "reply",
		},
		{
			name: "unknown purpose is a bound reply", reviewType: "UNKNOWN_PURPOSE",
			decision:  reviewdec.Decision{MissingFacts: []string{"transaction_semantics"}, AllowedActions: []string{"CONFIRM_REVIEW", "IGNORE"}, InteractionMode: reviewdec.ModeSingleField},
			wantState: "AWAITING_DETAIL", wantMessage: "🟡 Perlu detail transaksi\n\nNominal: Rp18.502\n\nBalas pesan ini dengan keterangan atau tujuan transaksi.", context: "Nominal: Rp18.502", wantMode: "reply",
		},
		{
			name: "transfer relationship gets the transfer chooser", reviewType: "TRANSFER_CLASSIFICATION",
			decision:  reviewdec.Decision{MissingFacts: []string{"transfer_relationship"}, AllowedActions: []string{"CLASSIFY_TRANSFER", "MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}, InteractionMode: reviewdec.ModeBoundedChoice},
			wantState: "AWAITING_DETAIL", wantMessage: "rincian transfer", context: "rincian transfer", wantMode: "transfer",
		},
		{
			name: "duplicate conflict offers duplicate intents", reviewType: "POSSIBLE_DUPLICATE",
			decision:  reviewdec.Decision{MissingFacts: []string{"duplicate_relationship"}, AllowedActions: []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}, InteractionMode: reviewdec.ModeBoundedChoice},
			wantState: "AWAITING_DETAIL", wantMessage: "🟡 Transaksi ini mungkin duplikat\n\ncandidate exists\n\nBalas pesan ini dengan pilihan pada tombol di atas.", context: "candidate exists", wantMode: "duplicate",
		},
		{
			name: "no decision fails closed to a detail prompt", reviewType: "SOMETHING_NEW",
			decision:  reviewdec.Decision{},
			wantState: "AWAITING_DETAIL", wantMessage: "🟡 Perlu detail transaksi\n\nkeep context\n\nBalas pesan ini dengan keterangan atau tujuan transaksi.", context: "keep context", wantMode: "reply",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, message, mode := renderReviewPresentation(tt.decision, tt.reviewType, tt.context)
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
