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

func TestAgentTurnResponsesUsesFunctionCallOutputContinuation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["previous_response_id"] != "resp-read" || request["instructions"] != "system" {
			t.Fatalf("continuation metadata = %#v", request)
		}
		input, ok := request["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("continuation input = %#v", request["input"])
		}
		output, _ := input[0].(map[string]any)
		if output["type"] != "function_call_output" || output["call_id"] != "read-1" {
			t.Fatalf("function output = %#v", output)
		}
		if _, ok := output["output"].(string); !ok {
			t.Fatalf("function output must be encoded string: %#v", output)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-final","model":"agent-model","output":[{"type":"message","content":[{"type":"output_text","text":"Pengeluaran naik karena dining."}]}]}`))
	}))
	defer server.Close()

	previous := ToolCall{ResponseID: "resp-read", CallID: "read-1", Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH"}`)}
	response, err := New(server.URL, "key", "agent-model").AgentTurn(context.Background(), "request", AgentRequest{
		SystemPrompt:       "system",
		Content:            "ignored for Responses continuation",
		Tools:              []ToolDefinition{{Name: "query_spending", Parameters: map[string]any{"type": "object"}}},
		PreviousResponseID: "resp-read",
		PreviousToolCalls:  []ToolCall{previous},
		ToolOutputs:        []AgentToolOutput{{CallID: "read-1", Output: map[string]any{"net_expense_idr": "100000"}}},
	})
	if err != nil {
		t.Fatalf("AgentTurn continuation error = %v", err)
	}
	if response.Text != "Pengeluaran naik karena dining." {
		t.Fatalf("response=%#v", response)
	}
}

func TestAgentTurnChatUsesAssistantAndToolMessagesForContinuation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		messages, ok := request["messages"].([]any)
		if !ok || len(messages) != 4 {
			t.Fatalf("messages=%#v", request["messages"])
		}
		assistant, _ := messages[2].(map[string]any)
		tool, _ := messages[3].(map[string]any)
		if assistant["role"] != "assistant" || tool["role"] != "tool" || tool["tool_call_id"] != "read-1" {
			t.Fatalf("native continuation messages assistant=%#v tool=%#v", assistant, tool)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat-final","model":"agent-model","choices":[{"message":{"content":"Sudah cukup datanya.","tool_calls":[]}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`))
	}))
	defer server.Close()

	previous := ToolCall{ResponseID: "chat-read", CallID: "read-1", Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH"}`)}
	response, err := NewWithProtocol(server.URL, "key", "agent-model", "chat_completions").AgentTurn(context.Background(), "request", AgentRequest{
		SystemPrompt:       "system",
		Content:            map[string]any{"turn_context": "original", "agent_tool_results": []any{}},
		Tools:              []ToolDefinition{{Name: "query_spending", Parameters: map[string]any{"type": "object"}}},
		PreviousResponseID: "chat-read",
		PreviousToolCalls:  []ToolCall{previous},
		ToolOutputs:        []AgentToolOutput{{CallID: "read-1", Output: map[string]any{"net_expense_idr": "100000"}}},
	})
	if err != nil {
		t.Fatalf("chat AgentTurn continuation error = %v", err)
	}
	if response.Text != "Sudah cukup datanya." {
		t.Fatalf("response=%#v", response)
	}
}

func TestAgentTurnRejectsContinuationWithMismatchedCallIDs(t *testing.T) {
	client := New("http://example.invalid", "key", "agent-model")
	_, err := client.AgentTurn(context.Background(), "request", AgentRequest{
		SystemPrompt:       "system",
		Content:            "x",
		PreviousResponseID: "resp",
		PreviousToolCalls:  []ToolCall{{CallID: "call-1", Name: "query_spending", Arguments: json.RawMessage(`{}`)}},
		ToolOutputs:        []AgentToolOutput{{CallID: "different", Output: map[string]any{"ok": true}}},
	})
	if err == nil {
		t.Fatal("mismatched continuation call IDs should fail before HTTP")
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

func TestAgentTurnFailureRecordsAgentCallKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var metric Metadata
	client := New(server.URL, "key", "agent-model")
	client.record = func(_ context.Context, call CallMetric) { metric.CallKind = call.CallKind }
	_, err := client.AgentTurn(context.Background(), "request", AgentRequest{SystemPrompt: "system", Content: "hello"})
	if err == nil {
		t.Fatal("expected gateway error")
	}
	if metric.CallKind != "AGENT_TEXT" {
		t.Fatalf("call kind=%q", metric.CallKind)
	}
}
