package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type Metadata struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         string
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
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
func (c *Client) NativeToolResult(ctx context.Context, requestID, responseID, callID, output string) (ToolResponse, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return ToolResponse{}, fmt.Errorf("LLM gateway is not configured")
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ToolResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolResponse{}, fmt.Errorf("LLM gateway returned HTTP %d", resp.StatusCode)
	}
	var env struct {
		ID, Model, Cost string
		Usage           struct {
			Input, Output int `json:"input_tokens"`
		} `json:"usage"`
		Output []struct {
			Type, Name, CallID string
			Arguments          json.RawMessage               `json:"arguments"`
			Content            []struct{ Type, Text string } `json:"content"`
		} `json:"output"`
	}
	if err = json.Unmarshal(raw, &env); err != nil {
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
func (c *Client) NativeToolCall(ctx context.Context, requestID, systemPrompt string, content any, tools []ToolDefinition) (ToolCall, Metadata, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway is not configured")
	}
	userContent, err := normalizeContent(content)
	if err != nil {
		return ToolCall{}, Metadata{}, err
	}
	encodedTools := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		encodedTools = append(encodedTools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters, "strict": true})
	}
	payload := map[string]any{"model": c.model, "input": []map[string]any{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userContent}}, "tools": encodedTools, "tool_choice": "auto", "stream": false}
	body, err := json.Marshal(payload)
	if err != nil {
		return ToolCall{}, Metadata{}, fmt.Errorf("encode tool request: %w", err)
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
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
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ToolCall{}, Metadata{}, err
	}
	for _, item := range envelope.Output {
		if item.Type == "function_call" && item.Name != "" && len(item.Arguments) > 0 {
			return ToolCall{ResponseID: envelope.ID, CallID: item.CallID, Name: item.Name, Arguments: item.Arguments}, Metadata{Model: envelope.Model, InputTokens: envelope.Usage.Input, OutputTokens: envelope.Usage.Output, Cost: envelope.Cost}, nil
		}
	}
	return ToolCall{}, Metadata{}, fmt.Errorf("LLM gateway returned no native function_call")
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) Structured(ctx context.Context, requestID, task, systemPrompt string, content any, schema map[string]any, out any) (Metadata, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return Metadata{}, fmt.Errorf("LLM gateway is not configured")
	}
	userContent, err := normalizeContent(content)
	if err != nil {
		return Metadata{}, err
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
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
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
	if err := json.Unmarshal(raw, &envelope); err != nil {
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
	if structured == "" || decoder.Decode(out) != nil {
		return Metadata{}, fmt.Errorf("LLM gateway returned invalid structured output")
	}
	return Metadata{Model: envelope.Model, InputTokens: envelope.Usage.Input, OutputTokens: envelope.Usage.Output, Cost: envelope.Cost}, nil
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
