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
	Required     bool
	MaxToolCalls int
	// ReasoningEffort is forwarded when the selected model supports it. Bank
	// extraction uses "none" so the required native-call response contains no
	// reasoning prose alongside the tool call.
	ReasoningEffort string
}

type ToolCall struct {
	ResponseID string
	CallID     string
	Name       string
	Arguments  json.RawMessage
}

type ToolResponse struct {
	ToolCall *ToolCall
	Text     string
	Metadata Metadata
}

// NativeToolResult submits a Go-produced function result and returns the next
// native call or final assistant text.
func (c *Client) NativeToolResult(ctx context.Context, requestID, responseID, callID, output string) (response ToolResponse, err error) {
	started := time.Now()
	defer func() { c.observe(ctx, started, response.Metadata, err) }()
	return c.nativeToolResult(ctx, requestID, responseID, callID, output)
}

func (c *Client) nativeToolResult(ctx context.Context, requestID, responseID, callID, output string) (ToolResponse, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return ToolResponse{}, fmt.Errorf("LLM gateway is not configured")
	}
	if c.protocol != "responses" {
		return ToolResponse{}, fmt.Errorf("native tool continuation requires responses protocol")
	}
	payload := map[string]any{"model": c.model, "previous_response_id": responseID, "input": []map[string]any{{"type": "function_call_output", "call_id": callID, "output": output}}, "stream": false}
	body, err := json.Marshal(payload)
	if err != nil {
		return ToolResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return ToolResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return ToolResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return ToolResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolResponse{}, fmt.Errorf("LLM gateway returned HTTP %d", resp.StatusCode)
	}
	var env struct {
		ID, Model, Cost string
		Usage           struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
		Output []struct {
			Type, Name, CallID string
			Arguments          json.RawMessage               `json:"arguments"`
			Content            []struct{ Type, Text string } `json:"content"`
		} `json:"output"`
	}
	if err = decodeStrict(raw, &env); err != nil {
		return ToolResponse{}, err
	}
	meta := Metadata{Model: env.Model, Cost: env.Cost, InputTokens: env.Usage.Input, OutputTokens: env.Usage.Output}
	for _, item := range env.Output {
		if item.Type == "function_call" {
			return ToolResponse{ToolCall: &ToolCall{ResponseID: env.ID, CallID: item.CallID, Name: item.Name, Arguments: item.Arguments}, Metadata: meta}, nil
		}
		for _, c := range item.Content {
			if c.Type == "output_text" {
				return ToolResponse{Text: c.Text, Metadata: meta}, nil
			}
		}
	}
	return ToolResponse{}, fmt.Errorf("LLM gateway returned no tool result")
}

// NativeToolCall asks the gateway for a native function_call. It never
// executes a tool; callers must validate and dispatch it in Go.
func (c *Client) NativeToolCall(ctx context.Context, requestID, systemPrompt string, content any, tools []ToolDefinition, optionValues ...NativeToolOptions) (call ToolCall, metadata Metadata, err error) {
	started := time.Now()
	defer func() { c.observe(ctx, started, metadata, err) }()
	return c.nativeToolCall(ctx, requestID, systemPrompt, content, tools, optionValues...)
}

func (c *Client) nativeToolCall(ctx context.Context, requestID, systemPrompt string, content any, tools []ToolDefinition, optionValues ...NativeToolOptions) (ToolCall, Metadata, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway is not configured")
	}
	userContent, err := normalizeContent(content)
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	options := NativeToolOptions{MaxToolCalls: 1}
	if len(optionValues) > 0 {
		options = optionValues[0]
	}
	if options.MaxToolCalls <= 0 {
		options.MaxToolCalls = 1
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
	payload := map[string]any{"model": c.model, "input": []map[string]any{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userContent}}, "tools": encodedTools, "tool_choice": toolChoice, "stream": false}
	if options.ReasoningEffort != "" {
		payload["reasoning_effort"] = options.ReasoningEffort
	}
	if options.MaxToolCalls == 1 {
		payload["parallel_tool_calls"] = false
	}
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
	var envelope struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Cost  string `json:"cost"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
		Output []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			CallID    string          `json:"call_id"`
			Arguments json.RawMessage `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := decodeStrict(raw, &envelope); err != nil {
		return ToolCall{}, Metadata{}, err
	}
	var calls []ToolCall
	if options.Required {
		for _, item := range envelope.Output {
			for _, content := range item.Content {
				if strings.TrimSpace(content.Text) != "" {
					return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned prose with required native tool call")
				}
			}
		}
	}
	for _, item := range envelope.Output {
		if item.Type == "function_call" {
			calls = append(calls, ToolCall{ResponseID: envelope.ID, CallID: item.CallID, Name: item.Name, Arguments: normalizeToolArguments(item.Arguments)})
		}
	}
	if len(calls) == 0 {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned no native tool call")
	}
	return validateNativeCalls(calls, allowed, options, Metadata{Model: envelope.Model, InputTokens: envelope.Usage.Input, OutputTokens: envelope.Usage.Output, Cost: envelope.Cost})
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
	if options.MaxToolCalls == 1 {
		payload["parallel_tool_calls"] = false
	}
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
	if options.Required {
		for _, choice := range env.Choices {
			if strings.TrimSpace(choice.Message.Content) != "" {
				return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned prose with required native tool call")
			}
		}
	}
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
	return validateNativeCalls(calls, allowed, options, Metadata{Model: env.Model, InputTokens: env.Usage.Input, OutputTokens: env.Usage.Output, Cost: env.Cost})
}

func validateNativeCalls(calls []ToolCall, allowed map[string]bool, options NativeToolOptions, metadata Metadata) (ToolCall, Metadata, error) {
	if len(calls) == 0 {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned no native tool call")
	}
	if len(calls) > options.MaxToolCalls {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned too many native tool calls")
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
	c.record(ctx, CallMetric{Task: c.task, Protocol: c.protocol, Model: model, Status: status, ErrorClass: errorClass, DurationMs: time.Since(started).Milliseconds(), InputTokens: metadata.InputTokens, OutputTokens: metadata.OutputTokens, Cost: metadata.Cost})
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

func (c *Client) Structured(ctx context.Context, requestID, task, systemPrompt string, content any, schema map[string]any, out any) (metadata Metadata, err error) {
	started := time.Now()
	defer func() { c.observe(ctx, started, metadata, err) }()
	return c.structured(ctx, requestID, task, systemPrompt, content, schema, out)
}

func (c *Client) structured(ctx context.Context, requestID, task, systemPrompt string, content any, schema map[string]any, out any) (Metadata, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return Metadata{}, fmt.Errorf("LLM gateway is not configured")
	}
	userContent, err := normalizeContent(content)
	if err != nil {
		return Metadata{}, err
	}
	if c.protocol == "chat_completions" {
		return c.structuredChatCompletion(ctx, requestID, systemPrompt, userContent, schema, out)
	}
	payload := map[string]any{
		"model": c.model,
		"input": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userContent},
		},
		"metadata": map[string]string{"task": task},
		"stream":   false,
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "telegram_finance_intent", "strict": true, "schema": schema,
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Metadata{}, fmt.Errorf("encode LLM request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Metadata{}, fmt.Errorf("create LLM request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	response, err := c.http.Do(req)
	if err != nil {
		return Metadata{}, fmt.Errorf("call LLM gateway: %w", err)
	}
	defer response.Body.Close()
	raw, err := readBounded(response.Body)
	if err != nil {
		return Metadata{}, fmt.Errorf("read LLM response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Metadata{}, fmt.Errorf("LLM gateway returned HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Model string `json:"model"`
		Cost  string `json:"cost"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := decodeStrict(raw, &envelope); err != nil {
		return Metadata{}, fmt.Errorf("decode LLM response: %w", err)
	}
	structured := ""
	for _, item := range envelope.Output {
		for _, content := range item.Content {
			if item.Type == "message" && content.Type == "output_text" {
				structured = content.Text
			}
		}
	}
	decoder := json.NewDecoder(strings.NewReader(structured))
	decoder.DisallowUnknownFields()
	if structured == "" || decoder.Decode(out) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Metadata{}, fmt.Errorf("LLM gateway returned invalid structured output")
	}
	return Metadata{Model: envelope.Model, InputTokens: envelope.Usage.Input, OutputTokens: envelope.Usage.Output, Cost: envelope.Cost}, nil
}

func (c *Client) structuredChatCompletion(ctx context.Context, requestID, systemPrompt string, userContent any, schema map[string]any, out any) (Metadata, error) {
	payload := map[string]any{"model": c.model, "messages": []map[string]any{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userContent}}, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "finance_output", "strict": true, "schema": schema}}, "stream": false}
	body, err := json.Marshal(payload)
	if err != nil {
		return Metadata{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Metadata{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return Metadata{}, fmt.Errorf("call chat completion gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return Metadata{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Metadata{}, fmt.Errorf("LLM chat completion returned HTTP %d", resp.StatusCode)
	}
	var env struct {
		Model, Cost string
		Usage       struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
		} `json:"usage"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := decodeStrict(raw, &env); err != nil {
		return Metadata{}, err
	}
	for _, choice := range env.Choices {
		if choice.Message.Content != "" {
			dec := json.NewDecoder(strings.NewReader(choice.Message.Content))
			dec.DisallowUnknownFields()
			if dec.Decode(out) == nil && dec.Decode(&struct{}{}) == io.EOF {
				return Metadata{Model: env.Model, InputTokens: env.Usage.Input, OutputTokens: env.Usage.Output, Cost: env.Cost}, nil
			}
		}
	}
	return Metadata{}, fmt.Errorf("LLM gateway returned invalid structured output")
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
