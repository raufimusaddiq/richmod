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
	_, _, err := client.NativeToolCall(context.Background(), "request-id", "system", "input", []ToolDefinition{{Name: "tool", Parameters: map[string]any{"type": "object"}}}, NativeToolOptions{Required: true})
	if err == nil || strings.Contains(err.Error(), apiKey) || strings.Contains(err.Error(), "provider leaked") {
		t.Fatalf("unsafe gateway error: %v", err)
	}
	if got := <-authorization; got != "Bearer "+apiKey {
		t.Fatalf("authorization = %q", got)
	}
}

func TestNativeToolCallAdaptsChatCompletionsEnvelopeWithAuxiliaryProse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","model":"router-model","choices":[{"message":{"content":"provider reasoning","tool_calls":[{"id":"call-1","type":"function","function":{"name":"create_transaction","arguments":"{\"type\":\"EXPENSE\",\"amount_idr\":\"40700\"}"}}]}}]}`))
	}))
	defer server.Close()
	call, metadata, err := NewWithProtocol(server.URL, "key", "primary", "chat_completions").NativeToolCall(context.Background(), "request", "system", "expense", []ToolDefinition{{Name: "create_transaction"}}, NativeToolOptions{Required: true})
	if err != nil {
		t.Fatalf("NativeToolCall() error = %v", err)
	}
	if call.Name != "create_transaction" || call.CallID != "call-1" || string(call.Arguments) == "" || metadata.Model != "router-model" {
		t.Fatalf("call=%+v metadata=%+v", call, metadata)
	}
}

func TestConfiguredProtocolNeverFallsBackOrDuplicatesRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "missing", http.StatusNotFound) }))
	defer server.Close()
	_, _, err := NewWithProtocol(server.URL, "key", "model", "responses").NativeToolCall(context.Background(), "request", "system", "input", []ToolDefinition{{Name: "tool"}})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRecorderReceivesRedactedCallMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["stream"] != true {
			t.Fatalf("stream=%#v", request["stream"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\",\"model\":\"router-model\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"fc-1\",\"type\":\"function_call\",\"name\":\"tool\",\"call_id\":\"call-1\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc-1\",\"delta\":\"{}\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"fc-1\",\"arguments\":\"{}\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"router-model\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3},\"cost\":\"0.001\"}}\n\n"))
	}))
	defer server.Close()
	recorded := make(chan CallMetric, 1)
	client := New(server.URL, "key", "configured-model").WithRecorder("BANK_EXTRACTION", func(_ context.Context, metric CallMetric) { recorded <- metric })
	if _, _, err := client.NativeToolCall(context.Background(), "request", "system", "private input", []ToolDefinition{{Name: "tool"}}); err != nil {
		t.Fatalf("NativeToolCall() error = %v", err)
	}
	metric := <-recorded
	if metric.Task != "BANK_EXTRACTION" || metric.Status != "SUCCEEDED" || metric.Model != "router-model" || metric.InputTokens != 12 || metric.OutputTokens != 3 || metric.Cost != "0.001" {
		t.Fatalf("metric = %#v", metric)
	}
}

func TestRecorderClassifiesFailureWithoutResponseContent(t *testing.T) {
	recorded := make(chan CallMetric, 1)
	client := New("", "", "configured-model").WithRecorder("GENERATE_INSIGHT", func(_ context.Context, metric CallMetric) { recorded <- metric })
	_, _, _ = client.NativeToolCall(context.Background(), "request", "system", "private input", []ToolDefinition{{Name: "tool"}})
	metric := <-recorded
	if metric.Status != "FAILED" || metric.ErrorClass != "NOT_CONFIGURED" || metric.InputTokens != 0 || metric.OutputTokens != 0 {
		t.Fatalf("metric = %#v", metric)
	}
}

func TestNativeToolCallRequiredRejectsProseWithoutToolAndSetsRequiredChoice(t *testing.T) {
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
	_, _, err := New(server.URL, "key", "primary").NativeToolCall(context.Background(), "request", "system", "email", []ToolDefinition{{Name: "emit_bank_transaction", Parameters: map[string]any{"type": "object"}}}, NativeToolOptions{Required: true, ReasoningEffort: "none"})
	if err == nil {
		t.Fatal("prose should fail closed")
	}
}

func TestNativeToolCallRequiredIgnoresAuxiliaryProseAlongsideResponsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[{"type":"message","content":[{"type":"output_text","text":"thinking aloud"}]},{"type":"function_call","name":"emit_bank_transaction","call_id":"call-1","arguments":"{}"}]}`))
	}))
	defer server.Close()
	call, _, err := New(server.URL, "key", "primary").NativeToolCall(context.Background(), "request", "system", "email", []ToolDefinition{{Name: "emit_bank_transaction"}}, NativeToolOptions{Required: true})
	if err != nil || call.Name != "emit_bank_transaction" {
		t.Fatalf("call=%+v err=%v", call, err)
	}
}

func TestValidateNativeCallsRejectsMultipleAndUnknown(t *testing.T) {
	valid := json.RawMessage(`{}`)
	if _, _, err := validateNativeCalls([]ToolCall{{Name: "a", Arguments: valid}, {Name: "a", Arguments: valid}}, map[string]bool{"a": true}, NativeToolOptions{}, Metadata{}); err == nil {
		t.Fatal("multiple calls should fail")
	}
	if _, _, err := validateNativeCalls([]ToolCall{{Name: "other", Arguments: valid}}, map[string]bool{"a": true}, NativeToolOptions{}, Metadata{}); err == nil {
		t.Fatal("unknown call should fail")
	}
}
