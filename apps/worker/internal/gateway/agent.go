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

// AgentToolOutput is one model-safe result returned for a provider-native tool
// call. Output may contain only the structured projection prepared by the Go
// finance layer; the gateway never reads canonical state itself.
type AgentToolOutput struct {
	CallID string
	Output any
}

// AgentRequest is the protocol-neutral contract used by the conversational
// finance lane. Unlike NativeToolCall, a conversational response may contain
// ordinary assistant text or multiple provider-native tool calls.
type AgentRequest struct {
	SystemPrompt    string
	Content         any
	Tools           []ToolDefinition
	AllowParallel   bool
	ReasoningEffort string

	// Continuation fields preserve provider-native tool-call semantics between
	// bounded model phases. Responses uses PreviousResponseID plus
	// function_call_output; Chat Completions reconstructs the immediately
	// preceding assistant tool_calls plus role=tool messages.
	PreviousResponseID string
	PreviousToolCalls  []ToolCall
	ToolOutputs        []AgentToolOutput
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
	if err := validateAgentContinuation(request); err != nil {
		return AgentResponse{}, err
	}

	encodedTools := make([]map[string]any, 0, len(request.Tools))
	allowed := make(map[string]bool, len(request.Tools))
	for _, tool := range request.Tools {
		encodedTools = append(encodedTools, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
			"strict":      true,
		})
		allowed[tool.Name] = true
	}
	if c.protocol == "chat_completions" {
		return c.agentChatCompletion(ctx, requestID, request, content, encodedTools, allowed)
	}

	payload := map[string]any{
		"model":  c.model,
		"stream": true,
	}
	if len(request.ToolOutputs) > 0 {
		inputs := make([]map[string]any, 0, len(request.ToolOutputs))
		for _, output := range request.ToolOutputs {
			encoded, encodeErr := encodeAgentToolOutput(output.Output)
			if encodeErr != nil {
				return AgentResponse{}, encodeErr
			}
			inputs = append(inputs, map[string]any{
				"type":    "function_call_output",
				"call_id": output.CallID,
				"output":  encoded,
			})
		}
		payload["previous_response_id"] = request.PreviousResponseID
		payload["instructions"] = request.SystemPrompt
		payload["input"] = inputs
	} else {
		payload["input"] = []map[string]any{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": content},
		}
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
	messages := []map[string]any{
		{"role": "system", "content": request.SystemPrompt},
		{"role": "user", "content": content},
	}
	if len(request.ToolOutputs) > 0 {
		toolCalls := make([]map[string]any, 0, len(request.PreviousToolCalls))
		for _, call := range request.PreviousToolCalls {
			toolCalls = append(toolCalls, map[string]any{
				"id":   call.CallID,
				"type": "function",
				"function": map[string]any{
					"name": call.Name, "arguments": string(call.Arguments),
				},
			})
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": nil, "tool_calls": toolCalls})
		for _, output := range request.ToolOutputs {
			encoded, encodeErr := encodeAgentToolOutput(output.Output)
			if encodeErr != nil {
				return AgentResponse{}, encodeErr
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": output.CallID, "content": encoded})
		}
	}
	payload := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   false,
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
		return AgentResponse{}, fmt.Errorf("call LLM chat completion gateway: %w", err)
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

func validateAgentContinuation(request AgentRequest) error {
	if len(request.ToolOutputs) == 0 {
		if request.PreviousResponseID != "" || len(request.PreviousToolCalls) != 0 {
			return fmt.Errorf("agent continuation metadata without tool outputs")
		}
		return nil
	}
	if request.PreviousResponseID == "" {
		return fmt.Errorf("agent continuation missing previous response id")
	}
	if len(request.PreviousToolCalls) == 0 {
		return fmt.Errorf("agent continuation missing previous tool calls")
	}
	known := make(map[string]bool, len(request.PreviousToolCalls))
	for _, call := range request.PreviousToolCalls {
		if call.CallID == "" || known[call.CallID] {
			return fmt.Errorf("agent continuation has invalid tool call id")
		}
		known[call.CallID] = true
	}
	if len(request.ToolOutputs) != len(known) {
		return fmt.Errorf("agent continuation tool output count mismatch")
	}
	seen := make(map[string]bool, len(request.ToolOutputs))
	for _, output := range request.ToolOutputs {
		if !known[output.CallID] || seen[output.CallID] {
			return fmt.Errorf("agent continuation contains unmatched tool output")
		}
		seen[output.CallID] = true
	}
	return nil
}

func encodeAgentToolOutput(output any) (string, error) {
	if text, ok := output.(string); ok {
		return text, nil
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("encode agent tool output: %w", err)
	}
	return string(encoded), nil
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
		if call.CallID == "" {
			return fmt.Errorf("LLM agent returned tool call without call id")
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
