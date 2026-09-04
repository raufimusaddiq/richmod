package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	baseURL  string
	apiKey   string
	model    string
	protocol string
	http     *http.Client
	task     string
	record   Recorder
}

type Metadata struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         string
	CallKind     string
	ToolName     string
}

// CallMetric contains metadata only. Prompt, response, and source content are
// intentionally excluded from operational telemetry.
type CallMetric struct {
	Task         string
	Protocol     string
	Model        string
	Status       string
	ErrorClass   string
	DurationMs   int64
	InputTokens  int
	OutputTokens int
	Cost         string
	CallKind     string
	ToolName     string
}

type Recorder func(context.Context, CallMetric)

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// NativeToolOptions controls the protocol-level tool contract. Optional
// options preserve the V3 call shape for existing conversational callers.
type NativeToolOptions struct {
	Required bool
	// ReasoningEffort is forwarded when the selected model supports it.
	ReasoningEffort string
}

type ToolCall struct {
	ResponseID string
	CallID     string
	Name       string
	Arguments  json.RawMessage
}

// NativeToolCall asks the gateway for a native function_call. It never
// executes a tool; callers must validate and dispatch it in Go.
func (c *Client) NativeToolCall(ctx context.Context, requestID, systemPrompt string, content any, tools []ToolDefinition, optionValues ...NativeToolOptions) (call ToolCall, metadata Metadata, err error) {
	started := time.Now()
	defer func() { c.observe(ctx, started, metadata, err) }()
	return c.nativeToolCall(ctx, requestID, systemPrompt, content, tools, optionValues...)
}

// DecodeToolArguments strictly decodes one provider-native function call.
// Callers still own domain validation after decoding.
func DecodeToolArguments[T any](call ToolCall, expectedName string) (T, error) {
	var out T
	if call.Name != expectedName {
		return out, fmt.Errorf("unexpected native tool %q", call.Name)
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return out, fmt.Errorf("decode %s arguments: %w", expectedName, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return out, fmt.Errorf("decode %s arguments: trailing JSON", expectedName)
	}
	return out, nil
}

func (c *Client) nativeToolCall(ctx context.Context, requestID, systemPrompt string, content any, tools []ToolDefinition, optionValues ...NativeToolOptions) (ToolCall, Metadata, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway is not configured")
	}
	userContent, err := normalizeContent(content)
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	options := NativeToolOptions{}
	if len(optionValues) > 0 {
		options = optionValues[0]
	}
	encodedTools := make([]map[string]any, 0, len(tools))
	allowed := make(map[string]bool, len(tools))
	for _, tool := range tools {
		encodedTools = append(encodedTools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters, "strict": true})
		allowed[tool.Name] = true
	}
	toolChoice := any("auto")
	if options.Required {
		toolChoice = "required"
	}
	payload := map[string]any{"model": c.model, "input": []map[string]any{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userContent}}, "tools": encodedTools, "tool_choice": toolChoice, "stream": true}
	if options.ReasoningEffort != "" {
		payload["reasoning_effort"] = options.ReasoningEffort
	}
	payload["parallel_tool_calls"] = false
	body, err := json.Marshal(payload)
	if err != nil {
		return ToolCall{}, Metadata{}, fmt.Errorf("encode tool request: %w", err)
	}
	if c.protocol == "chat_completions" {
		return c.nativeChatCompletion(ctx, requestID, systemPrompt, userContent, encodedTools, options)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return ToolCall{}, Metadata{}, fmt.Errorf("call LLM tool gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned HTTP %d", resp.StatusCode)
	}
	result, err := decodeResponses(raw)
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	call, metadata, err := validateNativeCalls(result.Calls, allowed, options, result.Metadata)
	metadata.CallKind = "NATIVE_TOOL"
	metadata.ToolName = call.Name
	return call, metadata, err
}

func normalizeToolArguments(raw json.RawMessage) json.RawMessage {
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		return json.RawMessage(encoded)
	}
	return raw
}

// nativeChatCompletion is the protocol adapter fallback for routers/models
// that expose OpenAI Chat Completions rather than Responses. It returns the
// same internal ToolCall shape, so callers do not need protocol-specific
// financial logic.
func (c *Client) nativeChatCompletion(ctx context.Context, requestID, systemPrompt string, userContent any, tools []map[string]any, options NativeToolOptions) (ToolCall, Metadata, error) {
	functions := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		fn := map[string]any{"type": "function", "function": map[string]any{"name": tool["name"], "description": tool["description"], "parameters": tool["parameters"]}}
		functions = append(functions, fn)
	}
	choice := any("auto")
	if options.Required {
		choice = "required"
	}
	payload := map[string]any{"model": c.model, "messages": []map[string]any{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userContent}}, "tools": functions, "tool_choice": choice, "stream": false}
	if options.ReasoningEffort != "" {
		payload["reasoning_effort"] = options.ReasoningEffort
	}
	payload["parallel_tool_calls"] = false
	return c.doChatToolCall(ctx, requestID, payload, options)
}

func (c *Client) doChatToolCall(ctx context.Context, requestID string, payload map[string]any, options NativeToolOptions) (ToolCall, Metadata, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return ToolCall{}, Metadata{}, fmt.Errorf("encode chat completion request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return ToolCall{}, Metadata{}, fmt.Errorf("call chat completion gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM chat completion returned HTTP %d", resp.StatusCode)
	}
	var env struct {
		ID, Model, Cost string
		Usage           struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
		} `json:"usage"`
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID, Type string
					Function struct{ Name, Arguments string } `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := decodeStrict(raw, &env); err != nil {
		return ToolCall{}, Metadata{}, err
	}
	var calls []ToolCall
	allowed := map[string]bool{}
	for _, raw := range payload["tools"].([]map[string]any) {
		fn := raw["function"].(map[string]any)
		allowed[fn["name"].(string)] = true
	}
	for _, choice := range env.Choices {
		for _, call := range choice.Message.ToolCalls {
			if call.Type == "function" {
				calls = append(calls, ToolCall{ResponseID: env.ID, CallID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
			}
		}
	}
	call, metadata, err := validateNativeCalls(calls, allowed, options, Metadata{Model: env.Model, InputTokens: env.Usage.Input, OutputTokens: env.Usage.Output, Cost: env.Cost})
	metadata.CallKind = "NATIVE_TOOL"
	metadata.ToolName = call.Name
	return call, metadata, err
}

func validateNativeCalls(calls []ToolCall, allowed map[string]bool, options NativeToolOptions, metadata Metadata) (ToolCall, Metadata, error) {
	if len(calls) != 1 {
		if len(calls) == 0 {
			return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned no native tool call")
		}
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned multiple native tool calls")
	}
	for _, call := range calls {
		if !allowed[call.Name] {
			return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned unknown tool")
		}
		if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
			return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned invalid tool arguments")
		}
	}
	return calls[0], metadata, nil
}

func New(baseURL, apiKey, model string) *Client {
	protocol := strings.TrimSpace(os.Getenv("LLM_GATEWAY_PROTOCOL"))
	if protocol == "" {
		protocol = "responses"
	}
	return NewWithProtocol(baseURL, apiKey, model, protocol)
}

func NewWithProtocol(baseURL, apiKey, model, protocol string) *Client {
	if protocol != "responses" && protocol != "chat_completions" {
		protocol = "responses"
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, protocol: protocol, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) WithRecorder(task string, record Recorder) *Client {
	clone := *c
	clone.task = task
	clone.record = record
	return &clone
}

func (c *Client) observe(ctx context.Context, started time.Time, metadata Metadata, callErr error) {
	if c.record == nil {
		return
	}
	status, errorClass := "SUCCEEDED", ""
	if callErr != nil {
		status, errorClass = "FAILED", metricErrorClass(callErr)
	}
	model := metadata.Model
	if model == "" {
		model = c.model
	}
	c.record(ctx, CallMetric{Task: c.task, Protocol: c.protocol, Model: model, Status: status, ErrorClass: errorClass, DurationMs: time.Since(started).Milliseconds(), InputTokens: metadata.InputTokens, OutputTokens: metadata.OutputTokens, Cost: metadata.Cost, CallKind: metadata.CallKind, ToolName: metadata.ToolName})
}

func metricErrorClass(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout") || strings.Contains(message, "deadline"):
		return "TIMEOUT"
	case strings.Contains(message, "http"):
		return "HTTP_ERROR"
	case strings.Contains(message, "decode") || strings.Contains(message, "invalid") || strings.Contains(message, "trailing json"):
		return "INVALID_RESPONSE"
	case strings.Contains(message, "not configured"):
		return "NOT_CONFIGURED"
	default:
		return "GATEWAY_ERROR"
	}
}

func readBounded(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, (2<<20)+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read LLM response: %w", err)
	}
	if len(raw) > 2<<20 {
		return nil, fmt.Errorf("LLM gateway response exceeds 2 MiB")
	}
	return raw, nil
}

func decodeStrict(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode LLM response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode LLM response: trailing JSON")
	}
	return nil
}

func normalizeContent(content any) (any, error) {
	switch content.(type) {
	case string, []map[string]any:
		return content, nil
	default:
		encoded, err := json.Marshal(content)
		if err != nil {
			return nil, fmt.Errorf("encode LLM user content: %w", err)
		}
		return string(encoded), nil
	}
}
