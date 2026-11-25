package telegram

import "testing"

func TestAssistantNeverAnswersATransferCard(t *testing.T) {
	for _, tool := range NativeFinanceTools(nil, false, false) {
		if tool.Name == "resolve_review" {
			t.Fatal("the assistant can still answer a review card")
		}
	}
}
