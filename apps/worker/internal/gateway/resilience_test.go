package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func sseStream(events ...string) []byte {
	var out strings.Builder
	for _, event := range events {
		out.WriteString("data: " + event + "\n\n")
	}
	return []byte(out.String())
}

func TestResponsesSSEFillsArgumentsFromEveryStreamedSource(t *testing.T) {
	created := `{"type":"response.created","response":{"id":"resp-1","model":"router-model"}}`
	added := `{"type":"response.output_item.added","item":{"id":"fc-1","type":"function_call","name":"emit","call_id":"call-1"}}`
	completed := `{"type":"response.completed","response":{"id":"resp-1","model":"router-model","usage":{"input_tokens":12,"output_tokens":3},"cost":"0.002"}}`
	cases := map[string]struct {
		events []string
		want   string
	}{
		"argument deltas": {[]string{created, added,
			`{"type":"response.function_call_arguments.delta","item_id":"fc-1","delta":"{\"amount\":"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc-1","delta":"1}"}`, completed}, `{"amount":1}`},
		"output item done": {[]string{created, added,
			`{"type":"response.output_item.done","item":{"id":"fc-1","type":"function_call","name":"emit","call_id":"call-1","arguments":"{\"amount\":2}"}}`, completed}, `{"amount":2}`},
		"completed output": {[]string{created, added,
			`{"type":"response.completed","response":{"id":"resp-1","model":"router-model","usage":{"input_tokens":12,"output_tokens":3},"cost":"0.002","output":[{"id":"fc-1","type":"function_call","name":"emit","call_id":"call-1","arguments":"{\"amount\":3}"}]}}`}, `{"amount":3}`},
		"final arguments beat deltas": {[]string{created, added,
			`{"type":"response.function_call_arguments.delta","item_id":"fc-1","delta":"{\"amount\":"}`,
			`{"type":"response.function_call_arguments.done","item_id":"fc-1","arguments":"{\"amount\":4}"}`, completed}, `{"amount":4}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := decodeResponses(sseStream(tc.events...))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Calls) != 1 || result.Calls[0].Name != "emit" || result.Calls[0].CallID != "call-1" || string(result.Calls[0].Arguments) != tc.want {
				t.Fatalf("calls = %+v, want arguments %s", result.Calls, tc.want)
			}
			if result.Metadata.InputTokens != 12 || result.Metadata.Cost != "0.002" || result.Metadata.Model != "router-model" {
				t.Fatalf("metadata lost: %+v", result.Metadata)
			}
		})
	}
}

func TestResponsesSSECompletedKeepsStreamedTextAndMetadata(t *testing.T) {
	result, err := decodeResponses(sseStream(
		`{"type":"response.created","response":{"id":"resp-1","model":"router-model"}}`,
		`{"type":"response.output_text.delta","delta":"halo"}`,
		`{"type":"response.completed","response":{"id":"resp-1","usage":{"input_tokens":5,"output_tokens":2},"output":[{"type":"message","content":[{"type":"output_text","text":"halo dunia"}]}]}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "halo" || result.Metadata.InputTokens != 5 || result.Metadata.Model != "router-model" {
		t.Fatalf("result = %+v", result)
	}
	only, err := decodeResponses(sseStream(
		`{"type":"response.completed","response":{"id":"resp-2","usage":{"input_tokens":7,"output_tokens":1},"output":[{"type":"message","content":[{"type":"output_text","text":"cukup"}]}]}}`,
	))
	if err != nil || only.Text != "cukup" || only.Metadata.InputTokens != 7 {
		t.Fatalf("completed-only result = %+v err=%v", only, err)
	}
}

func TestMidStreamResetIsClassifiedUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\"}\n\n"))
	}))
	defer server.Close()
	for _, protocol := range []string{"responses", "chat_completions"} {
		client := NewWithProtocol(server.URL, "key", "model", protocol)
		_, _, err := client.NativeToolCall(context.Background(), "r", "system", "x", []ToolDefinition{{Name: "tool"}})
		if !IsUnavailable(err) {
			t.Fatalf("%s native: a connection reset mid-body must be unavailable, got %v", protocol, err)
		}
		if _, err := client.AgentTurn(context.Background(), "r", AgentRequest{SystemPrompt: "s", Content: "x"}); !IsUnavailable(err) {
			t.Fatalf("%s agent: a connection reset mid-body must be unavailable, got %v", protocol, err)
		}
	}
}

func TestStatusErrorCarriesRetryAfterHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "90")
		http.Error(w, strings.Repeat("overloaded ", 1000), http.StatusTooManyRequests)
	}))
	defer server.Close()
	for _, protocol := range []string{"responses", "chat_completions"} {
		_, _, err := NewWithProtocol(server.URL, "key", "model", protocol).NativeToolCall(context.Background(), "r", "system", "x", []ToolDefinition{{Name: "tool"}})
		var hinted interface{ RetryAfter() time.Duration }
		if !errors.As(err, &hinted) || hinted.RetryAfter() != 90*time.Second || !IsUnavailable(err) {
			t.Fatalf("%s: Retry-After was not carried on %v", protocol, err)
		}
	}
	if _, err := New(server.URL, "key", "model").AgentTurn(context.Background(), "r", AgentRequest{SystemPrompt: "s", Content: "x"}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("agent turn error = %v", err)
	}
}

func TestParseRetryAfterAcceptsSecondsAndHTTPDates(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":          0,
		"0":         0,
		"-5":        0,
		"garbage":   0,
		"30":        30 * time.Second,
		" 45 ":      45 * time.Second,
		"999999999": 24 * time.Hour,
		now.Add(2 * time.Minute).Format(http.TimeFormat):  2 * time.Minute,
		now.Add(-2 * time.Minute).Format(http.TimeFormat): 0,
	}
	for value, want := range cases {
		if got := ParseRetryAfter(value, now); got != want {
			t.Errorf("ParseRetryAfter(%q) = %v, want %v", value, got, want)
		}
	}
}

func captureChatRequest(t *testing.T, requests chan<- map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat-1","model":"m","choices":[{"message":{"content":"selesai","tool_calls":[{"id":"call-1","type":"function","function":{"name":"emit","arguments":"{}"}}]}}]}`))
	}))
}

func TestChatCompletionsKeepsStrictToolContract(t *testing.T) {
	requests := make(chan map[string]any, 4)
	server := captureChatRequest(t, requests)
	defer server.Close()
	client := NewWithProtocol(server.URL, "key", "model", "chat_completions")
	tools := []ToolDefinition{{Name: "emit", Parameters: map[string]any{"type": "object"}}}
	if _, _, err := client.NativeToolCall(context.Background(), "r", "system", "x", tools); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentTurn(context.Background(), "r", AgentRequest{SystemPrompt: "s", Content: "x", Tools: tools}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.NativeToolCall(context.Background(), "r", "system", "x", tools, NativeToolOptions{DisableStrict: true}); err != nil {
		t.Fatal(err)
	}
	for i, wantStrict := range []bool{true, true, false} {
		request := <-requests
		function := request["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		strict, present := function["strict"].(bool)
		if present != wantStrict || (present && !strict) {
			t.Fatalf("request %d: strict=%v present=%v, want strict flag %v", i, strict, present, wantStrict)
		}
	}
}

func TestAgentChatContinuationDoesNotDuplicateToolResults(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := captureChatRequest(t, requests)
	defer server.Close()
	call := ToolCall{ResponseID: "chat-read", CallID: "read-1", Name: "emit", Arguments: json.RawMessage(`{}`)}
	_, err := NewWithProtocol(server.URL, "key", "model", "chat_completions").AgentTurn(context.Background(), "r", AgentRequest{
		SystemPrompt:       "system",
		Content:            map[string]any{"turn_context": "original", "agent_tool_results": []any{map[string]any{"net_expense_idr": "100000"}}},
		Tools:              []ToolDefinition{{Name: "emit"}},
		PreviousResponseID: "chat-read",
		PreviousToolCalls:  []ToolCall{call},
		ToolOutputs:        []AgentToolOutput{{CallID: "read-1", Output: map[string]any{"net_expense_idr": "100000"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	user := request["messages"].([]any)[1].(map[string]any)["content"].(string)
	if strings.Contains(user, "agent_tool_results") || !strings.Contains(user, "turn_context") {
		t.Fatalf("user turn must keep context but not replay tool results: %s", user)
	}
	if tool := request["messages"].([]any)[3].(map[string]any); tool["role"] != "tool" {
		t.Fatalf("tool result must be carried by the tool message: %#v", tool)
	}
}

func TestValidationFailureStillReportsTokensAndModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"actual-model","cost":"0.004","usage":{"input_tokens":21,"output_tokens":8},"output":[{"type":"function_call","name":"drop_database","call_id":"c","arguments":"{}"}]}`))
	}))
	defer server.Close()
	var native, agent []CallMetric
	record := func(into *[]CallMetric) Recorder {
		return func(_ context.Context, metric CallMetric) { *into = append(*into, metric) }
	}
	tools := []ToolDefinition{{Name: "emit"}}
	_, _, err := New(server.URL, "key", "configured").WithRecorder("BANK_EXTRACTION", record(&native)).NativeToolCall(context.Background(), "r", "system", "x", tools)
	if err == nil {
		t.Fatal("unknown tool must fail")
	}
	_, err = New(server.URL, "key", "configured").WithRecorder("TELEGRAM_NATIVE", record(&agent)).AgentTurn(context.Background(), "r", AgentRequest{SystemPrompt: "s", Content: "x", Tools: tools})
	if err == nil {
		t.Fatal("unknown agent tool must fail")
	}
	for name, metrics := range map[string][]CallMetric{"native": native, "agent": agent} {
		if len(metrics) != 1 || metrics[0].Status != "FAILED" || metrics[0].Model != "actual-model" || metrics[0].InputTokens != 21 || metrics[0].OutputTokens != 8 || metrics[0].Cost != "0.004" {
			t.Fatalf("%s metric lost spend on a validation failure: %+v", name, metrics)
		}
	}
}

func TestMetricErrorClassSeparatesTransportFromStatus(t *testing.T) {
	transport := &UnavailableError{Err: fmt.Errorf("call LLM gateway: %w", &url.Error{Op: "Post", URL: "https://llm.internal.example/v1/responses", Err: errors.New("connection refused")})}
	cases := map[string]struct {
		err  error
		want string
	}{
		"transport failure": {transport, "UNAVAILABLE"},
		"http status":       {&StatusError{StatusCode: 503, Endpoint: "responses"}, "HTTP_ERROR"},
		"not configured":    {ErrNotConfigured, "NOT_CONFIGURED"},
		"deadline":          {fmt.Errorf("call: %w", context.DeadlineExceeded), "TIMEOUT"},
		"decode":            {errors.New("decode LLM response: trailing JSON"), "INVALID_RESPONSE"},
	}
	for name, tc := range cases {
		if got := metricErrorClass(tc.err); got != tc.want {
			t.Errorf("%s: metricErrorClass = %s, want %s", name, got, tc.want)
		}
	}
}

func TestNonSourceRequestKeepsRequestIDOutOfSourceTelemetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"m","output":[{"type":"function_call","name":"emit","call_id":"c","arguments":"{}"}]}`))
	}))
	defer server.Close()
	var seen []string
	client := New(server.URL, "key", "model").WithRecorder("GENERATE_INSIGHT", func(_ context.Context, metric CallMetric) { seen = append(seen, metric.SourceEventID) })
	tools := []ToolDefinition{{Name: "emit"}}
	if _, _, err := client.NativeToolCall(context.Background(), "request-1", "system", "x", tools); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.NativeToolCall(WithSourceEvent(context.Background(), "event-1"), "request-2", "system", "x", tools); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.NativeToolCall(WithNonSourceRequest(WithSourceEvent(context.Background(), "event-2")), "request-3", "system", "x", tools); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentTurn(WithNonSourceRequest(context.Background()), "insight-id", AgentRequest{SystemPrompt: "s", Content: "x", Tools: tools}); err != nil {
		t.Fatal(err)
	}
	want := []string{"request-1", "event-1", "", ""}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("source event ids = %q, want %q", seen, want)
		}
	}
}
