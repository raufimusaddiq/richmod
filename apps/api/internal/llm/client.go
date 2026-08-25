package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 4 << 20

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	models     map[string]string
}

type StructuredRequest struct {
	RequestID    string
	Task         string
	ModelPolicy  string
	SystemPrompt string
	UserContent  any
	SchemaName   string
	Schema       map[string]any
}

type Metadata struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         string
}

func NewClient(baseURL, apiKey string, models map[string]string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("LLM gateway URL and API key are required")
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		models:     models,
	}, nil
}

func (c *Client) Structured(ctx context.Context, request StructuredRequest, out any) (Metadata, error) {
	model, ok := c.models[request.ModelPolicy]
	if !ok || model == "" {
		return Metadata{}, fmt.Errorf("unknown model policy %q", request.ModelPolicy)
	}
	if request.SchemaName == "" || len(request.Schema) == 0 || out == nil {
		return Metadata{}, fmt.Errorf("schema name, schema, and output destination are required")
	}

	payload := map[string]any{
		"model": model,
		"input": []map[string]any{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": request.UserContent},
		},
		"stream": false,
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   request.SchemaName,
				"strict": true,
				"schema": request.Schema,
			},
		},
	}
	if request.Task != "" {
		payload["metadata"] = map[string]string{"task": request.Task}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Metadata{}, fmt.Errorf("encode gateway request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Metadata{}, fmt.Errorf("create gateway request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if request.RequestID != "" {
		req.Header.Set("X-Request-ID", request.RequestID)
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return Metadata{}, fmt.Errorf("call gateway: %w", err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return Metadata{}, fmt.Errorf("read gateway response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Metadata{}, fmt.Errorf("gateway returned HTTP %d", response.StatusCode)
	}

	var envelope struct {
		Model  string `json:"model"`
		Cost   string `json:"cost"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Metadata{}, fmt.Errorf("decode gateway envelope: %w", err)
	}

	structuredText := ""
	for _, item := range envelope.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				structuredText = content.Text
				break
			}
		}
	}
	if structuredText == "" {
		return Metadata{}, fmt.Errorf("gateway returned no structured output")
	}

	decoder := json.NewDecoder(strings.NewReader(structuredText))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return Metadata{}, fmt.Errorf("decode structured output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Metadata{}, err
	}

	return Metadata{
		Model:        envelope.Model,
		InputTokens:  envelope.Usage.Input,
		OutputTokens: envelope.Usage.Output,
		Cost:         envelope.Cost,
	}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode structured output trailer: %w", err)
	}
	return fmt.Errorf("decode structured output: multiple JSON values")
}
