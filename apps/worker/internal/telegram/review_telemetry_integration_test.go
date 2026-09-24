package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestPlainTelegramConfirmAndRejectCountAsBoundedChoices(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review-telemetry-terminal-actions")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)
	for _, action := range []struct {
		name, amount string
		messageID    int64
		args         map[string]any
	}{
		{name: "IGNORE", amount: "70000", messageID: 101, args: map[string]any{"action": "IGNORE"}},
		{name: "CONFIRM", amount: "90000", messageID: 202, args: map[string]any{"action": "CONFIRM", "category_slug": "dining"}},
	} {
		reviewID, _ := createAgentTransactionReview(t, ctx, f, action.amount, action.messageID)
		state := *f.state
		state.Update.Message.ReplyToMessage = &struct {
			MessageID int64 `json:"message_id"`
		}{MessageID: action.messageID}
		binding, err := p.exactAgentReviewBinding(ctx, f.householdID, f.chatID, action.messageID)
		mustAgentTest(t, err)
		if binding == nil || binding.ReviewRequestID != reviewID {
			t.Fatalf("%s binding=%#v, want review %s", action.name, binding, reviewID)
		}
		state.ReviewBinding = binding
		state.ReviewBindingCount = 1
		result, _, err := p.agentResolveBoundReview(ctx, &state, gateway.ToolCall{CallID: "telemetry-" + action.name, Name: "resolve_review"}, action.args)
		mustAgentTest(t, err)
		if result.Status != "RESOLVED" {
			t.Fatalf("%s result=%+v", action.name, result)
		}
	}
	var turns, bounded int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(bounded_choices),0) FROM product_telemetry_event WHERE household_id=$1 AND event_type='REVIEW_TURN'`, f.householdID).Scan(&turns, &bounded))
	if turns != 2 || bounded != 2 {
		t.Fatalf("plain terminal review actions turns=%d bounded=%d; want 2/2", turns, bounded)
	}
}
