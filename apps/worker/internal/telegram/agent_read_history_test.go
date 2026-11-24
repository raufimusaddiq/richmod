package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type multiPhaseCaptureGateway struct {
	requests  []gateway.AgentRequest
	responses []gateway.AgentResponse
	stopErr   error
}

func (g *multiPhaseCaptureGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.requests = append(g.requests, request)
	index := len(g.requests) - 1
	if index >= len(g.responses) {
		return gateway.AgentResponse{}, g.stopErr
	}
	return g.responses[index], nil
}

func TestRunAgentLoopRetainsReadHistoryAcrossPhases(t *testing.T) {
	stopErr := errors.New("stop after capturing third phase")
	argsThisMonth := map[string]any{"period": "THIS_MONTH", "from_date": nil, "to_date": nil}
	argsLastMonth := map[string]any{"period": "LAST_MONTH", "from_date": nil, "to_date": nil}
	rawThisMonth, _ := json.Marshal(argsThisMonth)
	rawLastMonth, _ := json.Marshal(argsLastMonth)

	model := &multiPhaseCaptureGateway{
		stopErr: stopErr,
		responses: []gateway.AgentResponse{
			{
				ResponseID: "resp-1",
				ToolCalls: []gateway.ToolCall{{
					CallID:    "call-1",
					Name:      "query_spending",
					Arguments: rawThisMonth,
				}},
			},
			{
				ResponseID: "resp-2",
				ToolCalls: []gateway.ToolCall{{
					CallID:    "call-2",
					Name:      "query_spending",
					Arguments: rawLastMonth,
				}},
			},
		},
	}
	state := &agentState{
		SourceEventID: "src-1",
		HouseholdID:   "hh-1",
		Now:           time.Date(2026, 11, 24, 10, 0, 0, 0, jakartaLocation()),
		Tools:         []gateway.ToolDefinition{{Name: "query_spending"}},
		TurnContext:   map[string]any{"current_user_text": "bandingkan pengeluaran bulan ini dan bulan lalu"},
		ReadCache: map[string]agentToolResult{
			readCacheKey("query_spending", argsThisMonth): {Tool: "query_spending", Class: agentToolRead, Status: "OK", Facts: map[string]any{"total": "100000"}},
			readCacheKey("query_spending", argsLastMonth): {Tool: "query_spending", Class: agentToolRead, Status: "OK", Facts: map[string]any{"total": "120000"}},
		},
	}
	p := &Processor{}
	err := p.runAgentLoop(context.Background(), model, state)
	if !errors.Is(err, stopErr) {
		t.Fatalf("err = %v, want %v", err, stopErr)
	}
	if len(model.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(model.requests))
	}
	if len(model.requests[1].ReadHistory) != 0 || len(model.requests[1].PreviousToolCalls) != 1 || model.requests[1].PreviousToolCalls[0].CallID != "call-1" {
		t.Fatalf("phase 2 continuation mismatch: %+v", model.requests[1])
	}
	third := model.requests[2]
	if len(third.ReadHistory) != 1 || len(third.ReadHistory[0].ToolCalls) != 1 || third.ReadHistory[0].ToolCalls[0].CallID != "call-1" {
		t.Fatalf("phase 3 missing phase 1 in ReadHistory: %+v", third.ReadHistory)
	}
	if len(third.PreviousToolCalls) != 1 || third.PreviousToolCalls[0].CallID != "call-2" || len(third.ToolOutputs) != 1 || third.ToolOutputs[0].CallID != "call-2" {
		t.Fatalf("phase 3 missing phase 2 continuation: calls=%+v outputs=%+v", third.PreviousToolCalls, third.ToolOutputs)
	}
}
