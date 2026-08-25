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
