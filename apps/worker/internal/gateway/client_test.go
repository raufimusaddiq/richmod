package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeContentSerializesStructuredDataAsText(t *testing.T) {
	content, err := normalizeContent(map[string]any{"message": "makan 50rb"})
	if err != nil {
		t.Fatalf("normalizeContent() error = %v", err)
	}
	text, ok := content.(string)
	if !ok {
		t.Fatalf("content type = %T", content)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil || decoded["message"] != "makan 50rb" {
		t.Fatalf("decoded = %#v, error = %v", decoded, err)
	}
}

func TestGatewayErrorNeverIncludesSecretOrResponseBody(t *testing.T) {
	const apiKey = "test-secret-api-key"
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		http.Error(w, "provider leaked "+apiKey, http.StatusBadGateway)
	}))
	defer server.Close()
	client := New(server.URL, apiKey, "test-model")
	_, err := client.Structured(context.Background(), "request-id", "test", "system", "input", map[string]any{"type": "object"}, &map[string]any{})
	if err == nil || strings.Contains(err.Error(), apiKey) || strings.Contains(err.Error(), "provider leaked") {
		t.Fatalf("unsafe gateway error: %v", err)
	}
	if got := <-authorization; got != "Bearer "+apiKey {
		t.Fatalf("authorization = %q", got)
	}
}

func TestNativeToolCallAdaptsChatCompletionsEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","model":"router-model","choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"create_transaction","arguments":"{\"type\":\"EXPENSE\",\"amount_idr\":\"40700\"}"}}]}}]}`))
	}))
	defer server.Close()
	call, metadata, err := New(server.URL, "key", "primary").NativeToolCall(context.Background(), "request", "system", "expense", []ToolDefinition{{Name: "create_transaction"}})
	if err != nil {
		t.Fatalf("NativeToolCall() error = %v", err)
	}
	if call.Name != "create_transaction" || call.CallID != "call-1" || string(call.Arguments) == "" || metadata.Model != "router-model" {
		t.Fatalf("call=%+v metadata=%+v", call, metadata)
	}
}

func TestNativeToolCallRequiredRejectsProseAndSetsRequiredChoice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Fatal("invalid request")
		}
		if request["tool_choice"] != "required" || request["parallel_tool_calls"] != false {
			t.Fatalf("request contract = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[{"type":"message","content":[{"type":"output_text","text":"not a tool"}]}]}`))
	}))
	defer server.Close()
	_, _, err := New(server.URL, "key", "primary").NativeToolCall(context.Background(), "request", "system", "email", []ToolDefinition{{Name: "emit_bank_transaction", Parameters: map[string]any{"type": "object"}}}, NativeToolOptions{Required: true, MaxToolCalls: 1, ReasoningEffort: "none"})
	if err == nil {
		t.Fatal("prose should fail closed")
	}
}

func TestNativeToolCallRequiredRejectsProseAlongsideResponsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[{"type":"message","content":[{"type":"output_text","text":"thinking aloud"}]},{"type":"function_call","name":"emit_bank_transaction","call_id":"call-1","arguments":"{}"}]}`))
	}))
	defer server.Close()
	_, _, err := New(server.URL, "key", "primary").NativeToolCall(context.Background(), "request", "system", "email", []ToolDefinition{{Name: "emit_bank_transaction"}}, NativeToolOptions{Required: true, MaxToolCalls: 1})
	if err == nil {
		t.Fatal("prose alongside a required tool call should fail closed")
	}
}

func TestValidateNativeCallsRejectsMultipleAndUnknown(t *testing.T) {
	valid := json.RawMessage(`{}`)
	if _, _, err := validateNativeCalls([]ToolCall{{Name: "a", Arguments: valid}, {Name: "a", Arguments: valid}}, map[string]bool{"a": true}, NativeToolOptions{MaxToolCalls: 1}, Metadata{}); err == nil {
		t.Fatal("multiple calls should fail")
	}
	if _, _, err := validateNativeCalls([]ToolCall{{Name: "other", Arguments: valid}}, map[string]bool{"a": true}, NativeToolOptions{MaxToolCalls: 1}, Metadata{}); err == nil {
		t.Fatal("unknown call should fail")
	}
}
