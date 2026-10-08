package telegram

import (
	"slices"
	"testing"
)

// The resolve_review arguments offered for a bound card are the ones its actions
// read, so the model cannot put the household's answer in a field the executor
// ignores.
func TestReviewToolArgumentsFollowTheCard(t *testing.T) {
	cases := []struct {
		name    string
		binding agentReviewBinding
		want    []string
	}{
		{"date card", agentReviewBinding{Kind: "TRANSACTION", ReviewType: "MISSING_TRANSACTION_DATE", ConversationState: "AWAITING_DATE"}, []string{"action", "transaction_at"}},
		{"purpose card", agentReviewBinding{Kind: "TRANSACTION", ReviewType: "UNKNOWN_PURPOSE", ConversationState: "AWAITING_DETAIL"}, []string{"action", "description"}},
		{"merchant card", agentReviewBinding{Kind: "TRANSACTION", ReviewType: "UNKNOWN_MERCHANT", ConversationState: "AWAITING_MERCHANT"}, []string{"action", "category_slug", "merchant", "wealth_account_hint"}},
		{"transfer card", agentReviewBinding{Kind: "TRANSACTION", ReviewType: "TRANSFER_CLASSIFICATION", ConversationState: "AWAITING_DETAIL"}, []string{"action", "category_slug", "wealth_account_hint"}},
		{"duplicate card", agentReviewBinding{Kind: "TRANSACTION", ReviewType: "POSSIBLE_DUPLICATE", ConversationState: "AWAITING_DETAIL"}, []string{"action", "candidate_ref", "description"}},
		{"bank facts card", agentReviewBinding{Kind: "BANK_FACTS", ReviewType: "UNKNOWN_BANK_TEMPLATE", ConversationState: "AWAITING_DETAIL"}, []string{"action", "amount_idr", "transaction_at"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := NativeFinanceTools([]string{"dining"}, false, false, true, tc.binding.ReviewType, false, false, tc.binding.Kind)
			focusReviewArguments(tools, &tc.binding)
			for _, tool := range tools {
				if tool.Name != "resolve_review" {
					continue
				}
				properties := tool.Parameters["properties"].(map[string]any)
				var got []string
				for name := range properties {
					got = append(got, name)
				}
				slices.Sort(got)
				required, _ := tool.Parameters["required"].([]string)
				slices.Sort(required)
				if !slices.Equal(got, tc.want) || !slices.Equal(required, tc.want) {
					t.Fatalf("properties=%v required=%v, want %v", got, required, tc.want)
				}
				return
			}
			t.Fatal("resolve_review not offered")
		})
	}
}

// A binding the focus does not understand keeps the full argument set.
func TestReviewToolArgumentsUnchangedForOtherBindings(t *testing.T) {
	tools := NativeFinanceTools([]string{"dining"}, false, false, true, "WEALTH_OBSERVATION", false, false, "WEALTH_OBSERVATION")
	focusReviewArguments(tools, &agentReviewBinding{Kind: "WEALTH_OBSERVATION", ReviewType: "WEALTH_OBSERVATION"})
	for _, tool := range tools {
		if tool.Name == "resolve_review" {
			if len(tool.Parameters["properties"].(map[string]any)) < 10 {
				t.Fatal("wealth review arguments were narrowed")
			}
		}
	}
}
