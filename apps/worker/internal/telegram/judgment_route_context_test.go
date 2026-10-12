package telegram

import (
	"strings"
	"testing"
)

// A short reply ("jam 3 sore") answers the assistant's last question. The route
// classifier must see that exchange, or it returns OTHER_OR_UNCLEAR and the turn
// loses its mutation tools, so the expense is never recorded.
func TestRouteRequestCarriesRecentConversation(t *testing.T) {
	p := &Processor{}
	state := &turnAgentContextState{RecentTurns: []PublicTurn{
		{Role: "USER", Text: "kemarin jajan cilok 20k jam 3"},
		{Role: "ASSISTANT", Text: "Jam 3 pagi atau sore?"},
		{Role: "ASSISTANT", Tool: "query_spending", Context: map[string]any{"secret": "x"}},
	}}
	request := p.initialJudgmentRequest("jam 3 sore", state, simpleTransactionCandidate{})
	payload, _ := request.State.(map[string]any)
	turns, ok := payload["recent_turns"].([]map[string]string)
	if !ok || len(turns) != 2 {
		t.Fatalf("recent_turns=%#v", payload["recent_turns"])
	}
	if turns[0]["role"] != "USER" || !strings.Contains(turns[0]["text"], "cilok") || turns[1]["text"] != "Jam 3 pagi atau sore?" {
		t.Fatalf("turns=%#v", turns)
	}
	if turns[0]["text"] == "kemarin jajan cilok 20k jam 3" {
		t.Fatal("user text must stay marked untrusted")
	}
	empty := p.initialJudgmentRequest("halo", &turnAgentContextState{}, simpleTransactionCandidate{})
	emptyPayload, _ := empty.State.(map[string]any)
	if _, has := emptyPayload["recent_turns"]; has {
		t.Fatal("no history must not add recent_turns")
	}
}
