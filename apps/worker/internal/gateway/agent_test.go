package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentTurnAllowsTextWithoutToolsAndOmitsToolContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if _, ok := request["tools"]; ok {
			t.Fatalf("text-only phase unexpectedly sent tools: %#v", request)
		}
		if _, ok := request["tool_choice"]; ok {
			t.Fatalf("text-only phase unexpectedly sent tool_choice: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","model":"agent-model","output":[{"type":"message","content":[{"type":"output_text","text":"Sama-sama."}]}]}`))
	}))
	defer server.Close()

	response, err := New(server.URL, "key", "agent-model").AgentTurn(context.Background(), "request", AgentRequest{SystemPrompt: "system", Content: "oke makasih"})
	if err != nil {
		t.Fatalf("AgentTurn() error = %v", err)
	}
	if response.Text != "Sama-sama." || len(response.ToolCalls) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestAgentTurnAllowsMultipleReadToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["tool_choice"] != "auto" || request["parallel_tool_calls"] != true {
			t.Fatalf("agent tool contract = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-2","model":"agent-model","output":[{"type":"function_call","name":"query_spending","call_id":"call-1","arguments":"{\"period\":\"THIS_MONTH\",\"from_date\":null,\"to_date\":null}"},{"type":"function_call","name":"get_category_breakdown","call_id":"call-2","arguments":"{\"period\":\"THIS_MONTH\",\"from_date\":null,\"to_date\":null}"}]}`))
	}))
	defer server.Close()

	tools := []ToolDefinition{
		{Name: "query_spending", Parameters: map[string]any{"type": "object"}},
		{Name: "get_category_breakdown", Parameters: map[string]any{"type": "object"}},
	}
	response, err := New(server.URL, "key", "agent-model").AgentTurn(context.Background(), "request", AgentRequest{SystemPrompt: "system", Content: "bulan ini boros?", Tools: tools, AllowParallel: true})
	if err != nil {
		t.Fatalf("AgentTurn() error = %v", err)
	}
	if len(response.ToolCalls) != 2 || response.ToolCalls[0].Name != "query_spending" || response.ToolCalls[1].Name != "get_category_breakdown" {
		t.Fatalf("calls = %#v", response.ToolCalls)
	}
}

func TestAgentTurnRejectsUnknownToolBeforeExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-3","output":[{"type":"function_call","name":"drop_database","call_id":"call-1","arguments":"{}"}]}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "key", "agent-model").AgentTurn(context.Background(), "request", AgentRequest{SystemPrompt: "system", Content: "x", Tools: []ToolDefinition{{Name: "query_spending"}}})
	if err == nil {
		t.Fatal("unknown tool should fail closed")
	}
}
