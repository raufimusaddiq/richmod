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
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:")) || bytes.Contains(raw, []byte("\ndata:")) {
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
	items := map[string]ToolCall{}
	order := make([]string, 0, 1)
	completed := false
	for _, block := range strings.Split(string(raw), "\n\n") {
		var data []string
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSuffix(line, "\r")
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
				key := event.Item.ID
				if key == "" {
					key = event.Item.CallID
				}
				if key != "" {
					if _, exists := items[key]; !exists {
						order = append(order, key)
					}
					items[key] = ToolCall{ResponseID: result.ID, CallID: event.Item.CallID, Name: event.Item.Name}
				}
			}
		case "response.function_call_arguments.done":
			key := event.ItemID
			if key == "" {
				key = event.CallID
			}
			call := items[key]
			if call.CallID == "" {
				call.CallID = event.CallID
			}
			if call.Name == "" {
				call.Name = event.Name
			}
			call.ResponseID = result.ID
			call.Arguments = normalizeToolArguments(event.Arguments)
			if key == "" {
				key = call.CallID
			}
			if key != "" {
				if _, exists := items[key]; !exists {
					order = append(order, key)
				}
				items[key] = call
			}
		case "response.output_text.delta":
			result.Text += event.Delta
		case "response.completed":
			completed = true
			mergeResponsesMetadata(&result, event.Response)
			if len(items) == 0 && len(event.Response.Output) > 0 {
				return resultFromEnvelope(event.Response), nil
			}
		case "response.failed", "error":
			return responsesResult{}, fmt.Errorf("LLM gateway stream failed")
		}
	}
	if !completed {
		return responsesResult{}, fmt.Errorf("LLM gateway stream ended before completion")
	}
	for _, key := range order {
		call := items[key]
		if call.ResponseID == "" {
			call.ResponseID = result.ID
		}
		result.Calls = append(result.Calls, call)
	}
	return result, nil
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
