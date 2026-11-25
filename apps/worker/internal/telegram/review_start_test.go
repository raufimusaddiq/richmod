package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestRenderReviewPresentationFollowsDecision(t *testing.T) {
	tests := []struct {
		name       string
		reviewType string
		decision   reviewdec.Decision
		wantState  string
		context    string
		wantMode   string
		subject    reviewSubject
	}{
		{
			name: "missing amount is an amount step", reviewType: "MISSING_AMOUNT",
			decision:  reviewdec.Decision{ReasonCode: "MISSING_AMOUNT", MissingFacts: []string{"amount"}, AllowedActions: []string{"SET_AMOUNT", "IGNORE"}},
			wantState: "AWAITING_AMOUNT", context: "Email bank", wantMode: "step",
		},
		{
			name: "salary classification offers bounded choices", reviewType: "PAYSLIP_CONFIRMATION",
			decision:  reviewdec.Decision{MissingFacts: []string{"salary_classification"}, AllowedActions: []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}},
			wantState: "AWAITING_DETAIL", context: "Slip gaji", wantMode: "step",
		},
		{
			name: "category only is a bounded chooser", reviewType: "UNKNOWN_MERCHANT",
			decision:  reviewdec.Decision{DecisionClass: reviewdec.ClassEvidenceGap, MissingFacts: []string{"category"}, AllowedActions: []string{"CONFIRM_REVIEW", "IGNORE"}, InteractionMode: reviewdec.ModeBoundedChoice},
			wantState: "AWAITING_CATEGORY", context: "keep context", wantMode: "step",
		},
		{
			name: "missing date is a date step", reviewType: "MISSING_TRANSACTION_DATE",
			decision:  reviewdec.Decision{MissingFacts: []string{"transaction_at"}, AllowedActions: []string{"SET_TRANSACTION_DATE", "IGNORE"}, InteractionMode: reviewdec.ModeSingleField},
			subject:   reviewSubject{TransactionType: "EXPENSE"},
			wantState: "AWAITING_DATE", context: "Nominal: Rp18.502", wantMode: "step",
		},
		{
			name: "compound residual collects the date first", reviewType: "TRANSACTION_FACTS_MISSING",
			decision:  reviewdec.Decision{MissingFacts: []string{"category", "transaction_at"}, AllowedActions: []string{"CONFIRM_REVIEW", "SET_TRANSACTION_DATE", "IGNORE"}, InteractionMode: reviewdec.ModeSingleField},
			subject:   reviewSubject{TransactionType: "EXPENSE"},
			wantState: "AWAITING_DATE", context: "Nominal: Rp18.502", wantMode: "step",
		},
		{
			name: "unknown purpose is a purpose step", reviewType: "UNKNOWN_PURPOSE",
			decision:  reviewdec.Decision{MissingFacts: []string{"transaction_semantics"}, AllowedActions: []string{"CONFIRM_REVIEW", "IGNORE"}, InteractionMode: reviewdec.ModeSingleField},
			subject:   reviewSubject{TransactionType: "EXPENSE"},
			wantState: "AWAITING_PURPOSE", context: "Nominal: Rp18.502", wantMode: "step",
		},
		{
			name: "duplicate conflict offers duplicate intents", reviewType: "POSSIBLE_DUPLICATE",
			decision:  reviewdec.Decision{MissingFacts: []string{"duplicate_relationship"}, AllowedActions: []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}, InteractionMode: reviewdec.ModeBoundedChoice},
			wantState: "AWAITING_DETAIL", context: "candidate exists", wantMode: "duplicate",
		},
		{
			name: "receipt mismatch asks only to accept the printed total", reviewType: "RECEIPT_MISMATCH",
			decision:  reviewdec.Decision{ReasonCode: "RECEIPT_MISMATCH", Consequence: reviewdec.QualitySignal, AllowedActions: []string{"CONFIRM_REVIEW", "IGNORE"}},
			wantState: "AWAITING_DETAIL", wantMode: "receipt_quality",
		},
		{
			name: "no decision fails closed to a detail prompt", reviewType: "SOMETHING_NEW",
			decision:  reviewdec.Decision{},
			wantState: "AWAITING_DETAIL", context: "keep context", wantMode: "reply",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, message, mode := renderReviewPresentation(tt.decision, tt.reviewType, tt.context, tt.subject)
			t.Logf("state=%s mode=%s\n%s", state, mode, message)
			if state != tt.wantState || mode != tt.wantMode {
				t.Fatalf("state=%q message=%q mode=%q", state, message, mode)
			}
			if strings.TrimSpace(message) == "" || (tt.context != "" && !strings.Contains(message, tt.context)) {
				t.Fatalf("rendered card lost its context: %q", message)
			}
		})
	}
	for _, status := range []string{"CONFIRMED", "NEEDS_REVIEW"} {
		t.Run("mutation "+status, func(t *testing.T) {
			message := agentMutationFallback(agentToolResult{Status: status, Mutation: map[string]any{"action": "TRANSACTION_RECORDED", "amount_idr": "55199"}})
			t.Log(message)
			if !strings.Contains(message, "Rp55.199") || (status == "NEEDS_REVIEW" && strings.Contains(message, "✅")) {
				t.Fatalf("mutation fallback lost amount or claimed pending success: %q", message)
			}
		})
	}
	for _, class := range []textFailureClass{textFailureTimeout, textFailureUnavailable, textFailureError} {
		message, reason := terminalTextFailureCopy(class)
		t.Logf("terminal reason=%s: %s", reason, message)
		if strings.TrimSpace(message) == "" || strings.Contains(message, "✅") {
			t.Fatalf("terminal failure missing message or claimed success: %q", message)
		}
	}
	t.Logf("help:\n%s", helpMessage)
}

func TestRequiredFieldReplyMarkupOnlyOffersIgnore(t *testing.T) {
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "t")
	card := buildReviewCard(decision, "MISSING_TRANSACTION_DATE", "", reviewSubject{})
	markup, err := reviewCardMarkup(context.Background(), nil, "", "MISSING_TRANSACTION_DATE", decision, card, reviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	if markup == nil || len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 || markup.InlineKeyboard[0][0].CallbackData != "review:ignore" {
		t.Fatalf("markup=%#v", markup)
	}
}
