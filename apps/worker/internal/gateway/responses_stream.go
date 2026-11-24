package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type responsesResult struct {
	ID       string
	Metadata Metadata
	Calls    []ToolCall
	Text     string
}

type responsesOutputItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type responsesEnvelope struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Cost   string `json:"cost"`
	Status string `json:"status"`
	Usage  struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
	Output []responsesOutputItem `json:"output"`
}

func decodeResponses(raw []byte) (responsesResult, error) {
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:")) || bytes.Contains(raw, []byte("\ndata:")) || bytes.Contains(raw, []byte("\r\ndata:")) {
		return decodeResponsesSSE(raw)
	}
	var envelope responsesEnvelope
	if err := decodeStrict(raw, &envelope); err != nil {
		return responsesResult{}, err
	}
	return resultFromEnvelope(envelope), nil
}

func resultFromEnvelope(envelope responsesEnvelope) responsesResult {
	result := responsesResult{ID: envelope.ID, Metadata: Metadata{Model: envelope.Model, InputTokens: envelope.Usage.Input, OutputTokens: envelope.Usage.Output, Cost: envelope.Cost}}
	for _, item := range envelope.Output {
		if item.Type == "function_call" {
			result.Calls = append(result.Calls, ToolCall{ResponseID: envelope.ID, CallID: item.CallID, Name: item.Name, Arguments: normalizeToolArguments(item.Arguments)})
		}
		if item.Type == "message" {
			for _, content := range item.Content {
				if content.Type == "output_text" {
					result.Text += content.Text
				}
			}
		}
	}
	return result
}

func decodeResponsesSSE(raw []byte) (responsesResult, error) {
	var result responsesResult
	state := &streamState{}
	completed := false
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, block := range strings.Split(normalized, "\n\n") {
		var data []string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if len(data) == 0 || strings.Join(data, "\n") == "[DONE]" {
			continue
		}
		var event struct {
			Type      string              `json:"type"`
			ItemID    string              `json:"item_id"`
			CallID    string              `json:"call_id"`
			Name      string              `json:"name"`
			Arguments json.RawMessage     `json:"arguments"`
			Delta     string              `json:"delta"`
			Item      responsesOutputItem `json:"item"`
			Response  responsesEnvelope   `json:"response"`
		}
		if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &event); err != nil {
			return responsesResult{}, fmt.Errorf("decode LLM response stream: %w", err)
		}
		switch event.Type {
		case "response.created", "response.in_progress":
			mergeResponsesMetadata(&result, event.Response)
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				state.fill(event.Item.ID, event.Item.CallID, event.Item.Name, nil, false)
			}
		case "response.function_call_arguments.delta":
			state.appendDelta(event.ItemID, event.CallID, event.Delta)
		case "response.function_call_arguments.done":
			state.fill(event.ItemID, event.CallID, event.Name, event.Arguments, true)
		case "response.output_item.done":
			if event.Item.Type == "function_call" {
				state.fill(event.Item.ID, event.Item.CallID, event.Item.Name, event.Item.Arguments, true)
			}
		case "response.output_text.delta":
			result.Text += event.Delta
		case "response.completed":
			completed = true
			mergeResponsesMetadata(&result, event.Response)
			for _, item := range event.Response.Output {
				switch item.Type {
				case "function_call":
					state.fill(item.ID, item.CallID, item.Name, item.Arguments, false)
				case "message":
					if result.Text == "" {
						result.Text = outputText(item)
					}
				}
			}
		case "response.failed", "error":
			return responsesResult{}, fmt.Errorf("LLM gateway stream failed")
		}
	}
	if !completed {
		return responsesResult{}, fmt.Errorf("LLM gateway stream ended before completion")
	}
	result.Calls = state.calls(result.ID)
	return result, nil
}

func outputText(item responsesOutputItem) string {
	var text string
	for _, content := range item.Content {
		if content.Type == "output_text" {
			text += content.Text
		}
	}
	return text
}

type streamCall struct {
	itemID    string
	call      ToolCall
	arguments strings.Builder
	final     bool
}

type streamState struct {
	entries []*streamCall
}

func (s *streamState) entry(itemID, callID string) *streamCall {
	for _, candidate := range s.entries {
		if itemID != "" && candidate.itemID == itemID {
			return candidate
		}
	}
	for _, candidate := range s.entries {
		if callID != "" && candidate.call.CallID == callID {
			return candidate
		}
	}
	created := &streamCall{itemID: itemID}
	s.entries = append(s.entries, created)
	return created
}

func (s *streamState) fill(itemID, callID, name string, arguments json.RawMessage, final bool) {
	if itemID == "" && callID == "" {
		return
	}
	entry := s.entry(itemID, callID)
	if entry.itemID == "" {
		entry.itemID = itemID
	}
	if entry.call.CallID == "" {
		entry.call.CallID = callID
	}
	if entry.call.Name == "" {
		entry.call.Name = name
	}
	if entry.final && !final {
		return
	}
	if normalized := normalizeToolArguments(arguments); len(normalized) > 0 && string(normalized) != "null" {
		entry.call.Arguments = normalized
		entry.final = final
	}
}

func (s *streamState) appendDelta(itemID, callID, delta string) {
	if itemID == "" && callID == "" {
		return
	}
	entry := s.entry(itemID, callID)
	if entry.call.CallID == "" {
		entry.call.CallID = callID
	}
	entry.arguments.WriteString(delta)
}

func (s *streamState) calls(responseID string) []ToolCall {
	calls := make([]ToolCall, 0, len(s.entries))
	for _, entry := range s.entries {
		call := entry.call
		call.ResponseID = responseID
		if len(call.Arguments) == 0 && entry.arguments.Len() > 0 {
			call.Arguments = json.RawMessage(entry.arguments.String())
		}
		calls = append(calls, call)
	}
	return calls
}

func mergeResponsesMetadata(result *responsesResult, envelope responsesEnvelope) {
	if envelope.ID != "" {
		result.ID = envelope.ID
	}
	if envelope.Model != "" {
		result.Metadata.Model = envelope.Model
	}
	if envelope.Cost != "" {
		result.Metadata.Cost = envelope.Cost
	}
	if envelope.Usage.Input != 0 || envelope.Usage.Output != 0 {
		result.Metadata.InputTokens = envelope.Usage.Input
		result.Metadata.OutputTokens = envelope.Usage.Output
	}
}
