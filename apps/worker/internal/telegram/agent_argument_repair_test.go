package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func badTransactionDetails(callID string) gateway.ToolCall {
	return gateway.ToolCall{ResponseID: "resp-1", CallID: callID, Name: "get_transaction_details", Arguments: []byte(`{"target_ref":""}`)}
}

// Go rejecting a call's arguments is not the end of the turn: the model is told
// why, once, and Go validates whatever it sends next.
func TestRejectedArgumentsAreReturnedToTheModelOnce(t *testing.T) {
	call := badTransactionDetails("c1")
	_, err := validateAgentCallSet([]gateway.ToolCall{call}, &agentState{}, defaultAgentLimits)
	if err == nil {
		t.Fatal("empty target_ref must be rejected")
	}
	state := &agentState{ModelPhases: 1}
	rejected, ok := repairableArguments(state, err)
	if !ok || rejected.Call.CallID != "c1" {
		t.Fatalf("a malformed argument must be repairable, got ok=%t", ok)
	}
	state.ArgumentRepairs = maxArgumentRepairs
	if _, ok := repairableArguments(state, err); ok {
		t.Fatal("a second rejection in the same turn must stay final")
	}
	if _, ok := repairableArguments(&agentState{ModelPhases: defaultAgentLimits.MaxModelPhases}, err); ok {
		t.Fatal("no repair once the turn has used every model phase")
	}
}

// Server-state decisions are not the model's to fix.
func TestOnlyArgumentRejectionsAreRepairable(t *testing.T) {
	state := &agentState{Tools: []gateway.ToolDefinition{{Name: "query_wealth"}}, ModelPhases: 1}
	for name, calls := range map[string][]gateway.ToolCall{
		"tool not available in this state": {{CallID: "c1", Name: "record_transaction", Arguments: []byte(`{}`)}},
		"unknown tool":                     {{CallID: "c1", Name: "drop_database", Arguments: []byte(`{}`)}},
		"empty set":                        nil,
	} {
		_, err := validateAgentCallSet(calls, state, defaultAgentLimits)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if _, ok := repairableArguments(state, err); ok {
			t.Fatalf("%s must not be handed back to the model", name)
		}
	}
	if _, ok := repairableArguments(&agentState{}, errors.New("database unavailable")); ok {
		t.Fatal("a non-validation error must not be repairable")
	}
}

func TestRepairOutputsAnswerEveryCallAndNameOnlyTheOffender(t *testing.T) {
	good := gateway.ToolCall{CallID: "c0", Name: "query_wealth", Arguments: []byte(`{}`)}
	bad := badTransactionDetails("c1")
	rejected := &agentArgumentsError{Call: bad, Err: errors.New("invalid transaction reference")}
	outputs := argumentRepairOutputs([]gateway.ToolCall{good, bad}, rejected)
	if len(outputs) != 2 || outputs[0].CallID != "c0" || outputs[1].CallID != "c1" {
		t.Fatalf("every call needs exactly one output, got %+v", outputs)
	}
	if outputs[0].Output.(map[string]any)["status"] != "NOT_EXECUTED" {
		t.Fatalf("an untouched call must be reported as not executed: %+v", outputs[0])
	}
	offender := outputs[1].Output.(map[string]any)
	if offender["status"] != "INVALID_ARGUMENTS" || offender["reason"] != "invalid transaction reference" {
		t.Fatalf("offender output: %+v", offender)
	}
}

type scriptedModel struct {
	requests []gateway.AgentRequest
	replies  []gateway.AgentResponse
}

func (m *scriptedModel) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	m.requests = append(m.requests, request)
	if len(m.requests) > len(m.replies) {
		return gateway.AgentResponse{}, errors.New("script exhausted")
	}
	return m.replies[len(m.requests)-1], nil
}

// The loop really sends the correction request, with the provider-native
// continuation (previous response and call IDs) the gateway requires.
func TestAgentLoopSendsTheRejectionBackToTheModel(t *testing.T) {
	model := &scriptedModel{replies: []gateway.AgentResponse{
		{ResponseID: "resp-1", ToolCalls: []gateway.ToolCall{badTransactionDetails("c1")}},
	}}
	state := &agentState{}
	_ = (&Processor{}).runAgentLoop(context.Background(), model, state)
	if len(model.requests) != 2 {
		t.Fatalf("expected the original and one correction request, got %d", len(model.requests))
	}
	repair := model.requests[1]
	if repair.PreviousResponseID != "resp-1" || len(repair.PreviousToolCalls) != 1 || len(repair.ToolOutputs) != 1 {
		t.Fatalf("correction request lost the continuation: %+v", repair)
	}
	if repair.ToolOutputs[0].Output.(map[string]any)["status"] != "INVALID_ARGUMENTS" {
		t.Fatalf("correction request must carry the rejection: %+v", repair.ToolOutputs)
	}
	if state.ArgumentRepairs != 1 {
		t.Fatalf("repair not counted: %d", state.ArgumentRepairs)
	}
}
