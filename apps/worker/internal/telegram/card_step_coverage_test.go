package telegram

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func bindingKindsFor(reviewType string) []string {
	switch {
	case reviewType == cycleReviewType:
		return []string{"CYCLE_RESIDUAL"}
	case payslipReviewType(reviewType), reviewType == missingAmountReviewType:
		return []string{cardKindProposal}
	case reviewType == "WEALTH_OBSERVATION_CONFIRMATION":
		return []string{"WEALTH_OBSERVATION"}
	case reviewType == bankReviewType:
		return []string{"BANK_FACTS"}
	case documentReviewType(reviewType), reviewType == "FINANCIAL_EMAIL_RESOLUTION", reviewType == "FINANCIAL_EMAIL_FACTS":
		return []string{cardKindEvidence}
	}
	return []string{"TRANSACTION"}
}

func conversationStatesFor(kind, reviewType string) []string {
	switch kind {
	case "CYCLE_RESIDUAL":
		return []string{"AWAITING_DETAIL", "AWAITING_CYCLE_ACCOUNT", "AWAITING_CYCLE_AMOUNT"}
	case cardKindProposal:
		return []string{"AWAITING_DETAIL", "AWAITING_DATE", "AWAITING_CONFIRMATION"}
	case "WEALTH_OBSERVATION":
		return []string{"AWAITING_DETAIL", "AWAITING_CONFIRMATION"}
	case "BANK_FACTS":
		return []string{"AWAITING_DETAIL", "AWAITING_DATE", "AWAITING_BANK_ACCOUNT", "AWAITING_CONFIRMATION"}
	case cardKindEvidence:
		return []string{"AWAITING_DETAIL", "AWAITING_CONFIRMATION", "AWAITING_CATEGORY"}
	}
	if evidenceReviewType(reviewType) {
		return []string{"AWAITING_DETAIL", "AWAITING_CONFIRMATION", "AWAITING_CATEGORY", "AWAITING_ASSET_WEALTH", "AWAITING_INVESTMENT_ACCOUNT", "AWAITING_MERCHANT_DECISION"}
	}
	return []string{"AWAITING_MERCHANT", "AWAITING_CATEGORY", "AWAITING_DATE", "AWAITING_MERCHANT_DECISION", "AWAITING_ASSET_WEALTH", "AWAITING_INVESTMENT_ACCOUNT"}
}

func TestEveryTelegramReviewKindHasAStepInEveryStateTheEngineLeavesItIn(t *testing.T) {
	for _, reviewType := range reviewdec.TelegramReasons() {
		for _, kind := range bindingKindsFor(reviewType) {
			for _, state := range conversationStatesFor(kind, reviewType) {
				card := agentReviewBinding{Kind: kind, ReviewType: reviewType, ConversationState: state, TargetID: "x", ReviewRequestID: "r"}
				if _, ok := stepForBinding(card); !ok {
					t.Errorf("%s (%s) in %s has no step, so the assistant cannot show it", reviewType, kind, state)
				}
			}
		}
	}
}
