package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AgentRequest is the protocol-neutral contract used by the conversational
// finance lane. Unlike NativeToolCall, a conversational response may contain
// ordinary assistant text or multiple provider-native tool calls.
type AgentRequest struct {
	SystemPrompt    string
	Content         any
	Tools           []ToolDefinition
	AllowParallel   bool
	ReasoningEffort string
}

// AgentResponse intentionally exposes only display text and native tool calls.
// Provider reasoning/auxiliary fields are never surfaced as finance data.
type AgentResponse struct {
	ResponseID string
	Text       string
	ToolCalls  []ToolCall
	Metadata   Metadata
}

// AgentTurn performs one conversational model phase. It never executes tools.
// Zero tool calls are valid only when non-empty assistant text is returned.
// One or more native calls are valid when every call names an allow-listed tool
// and contains valid JSON arguments.
func (c *Client) AgentTurn(ctx context.Context, requestID string, request AgentRequest) (response AgentResponse, err error) {
	started := time.Now()
	defer func() { c.observe(ctx, started, response.Metadata, err) }()
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return AgentResponse{}, fmt.Errorf("LLM gateway is not configured")
	}
	content, err := normalizeContent(request.Content)
	if err != nil {
		return AgentResponse{}, err
	}
	encodedTools := make([]map[string]any, 0, len(request.Tools))
	allowed := make(map[string]bool, len(request.Tools))
	for _, tool := range request.Tools {
		encodedTools = append(encodedTools, map[string]any{
			"type":       "function",
			"name":       tool.Name,
			"description": tool.Description,
			"parameters": tool.Parameters,
			"strict":     true,
		})
		allowed[tool.Name] = true
	}
	if c.protocol == "chat_completions" {
		return c.agentChatCompletion(ctx, requestID, request, content, encodedTools, allowed)
	}
	payload := map[string]any{
		"model": c.model,
		"input": []map[string]any{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": content},
		},
		"stream": true,
	}
	if len(encodedTools) > 0 {
		payload["tools"] = encodedTools
		payload["tool_choice"] = "auto"
		payload["parallel_tool_calls"] = request.AllowParallel
	}
	if request.ReasoningEffort != "" {
		payload["reasoning_effort"] = request.ReasoningEffort
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("encode agent request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return AgentResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("call LLM agent gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return AgentResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AgentResponse{}, fmt.Errorf("LLM gateway returned HTTP %d", resp.StatusCode)
	}
	result, err := decodeResponses(raw)
	if err != nil {
		return AgentResponse{}, err
	}
	response = AgentResponse{ResponseID: result.ID, Text: strings.TrimSpace(result.Text), ToolCalls: result.Calls, Metadata: result.Metadata}
	if err := validateAgentResponse(response, allowed); err != nil {
		return AgentResponse{}, err
	}
	response.Metadata.CallKind = "AGENT_TEXT"
	if len(response.ToolCalls) > 0 {
		response.Metadata.CallKind = "AGENT_TOOLS"
		response.Metadata.ToolName = joinedToolNames(response.ToolCalls)
	}
	return response, nil
}

func (c *Client) agentChatCompletion(ctx context.Context, requestID string, request AgentRequest, content any, tools []map[string]any, allowed map[string]bool) (AgentResponse, error) {
	functions := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		functions = append(functions, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": tool["name"], "description": tool["description"], "parameters": tool["parameters"],
			},
		})
	}
	payload := map[string]any{
		"model": c.model,
		"messages": []map[string]any{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": content},
		},
		"stream": false,
	}
	if len(functions) > 0 {
		payload["tools"] = functions
		payload["tool_choice"] = "auto"
		payload["parallel_tool_calls"] = request.AllowParallel
	}
	if request.ReasoningEffort != "" {
		payload["reasoning_effort"] = request.ReasoningEffort
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("encode chat agent request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AgentResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("call LLM chat agent gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return AgentResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AgentResponse{}, fmt.Errorf("LLM chat completion returned HTTP %d", resp.StatusCode)
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
		return AgentResponse{}, err
	}
	response := AgentResponse{ResponseID: env.ID, Metadata: Metadata{Model: env.Model, InputTokens: env.Usage.Input, OutputTokens: env.Usage.Output, Cost: env.Cost}}
	for _, choice := range env.Choices {
		if strings.TrimSpace(choice.Message.Content) != "" {
			if response.Text != "" {
				response.Text += "\n"
			}
			response.Text += strings.TrimSpace(choice.Message.Content)
		}
		for _, call := range choice.Message.ToolCalls {
			if call.Type == "function" {
				response.ToolCalls = append(response.ToolCalls, ToolCall{ResponseID: env.ID, CallID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
			}
		}
	}
	if err := validateAgentResponse(response, allowed); err != nil {
		return AgentResponse{}, err
	}
	response.Metadata.CallKind = "AGENT_TEXT"
	if len(response.ToolCalls) > 0 {
		response.Metadata.CallKind = "AGENT_TOOLS"
		response.Metadata.ToolName = joinedToolNames(response.ToolCalls)
	}
	return response, nil
}

func validateAgentResponse(response AgentResponse, allowed map[string]bool) error {
	if len(response.ToolCalls) == 0 {
		if strings.TrimSpace(response.Text) == "" {
			return fmt.Errorf("LLM agent returned neither text nor tool calls")
		}
		return nil
	}
	for _, call := range response.ToolCalls {
		if !allowed[call.Name] {
			return fmt.Errorf("LLM agent returned unknown tool %q", call.Name)
		}
		if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
			return fmt.Errorf("LLM agent returned invalid tool arguments")
		}
	}
	return nil
}

func joinedToolNames(calls []ToolCall) string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	value := strings.Join(names, ",")
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}
