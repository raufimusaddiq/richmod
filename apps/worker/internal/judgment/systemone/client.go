package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

const maxResponseBytes = 2 << 20

type Metric struct {
	Model         string
	Status        string
	ErrorClass    string
	DurationMs    int64
	QuestionCount int
}

type Recorder func(context.Context, Metric)

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	record  Recorder
}

func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) WithRecorder(record Recorder) *Client {
	clone := *c
	clone.record = record
	return &clone
}

func (c *Client) Evaluate(ctx context.Context, requestID string, input judgment.Request) (result judgment.Result, err error) {
	started := time.Now()
	defer func() {
		if c.record != nil {
			status, class := "SUCCEEDED", ""
			if err != nil {
				status, class = "FAILED", classify(err)
			}
			c.record(ctx, Metric{Model: result.Model, Status: status, ErrorClass: class, DurationMs: time.Since(started).Milliseconds(), QuestionCount: len(input.Questions)})
		}
	}()
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return result, fmt.Errorf("System One gateway is not configured")
	}
	if len(input.Questions) == 0 {
		return result, fmt.Errorf("System One request has no questions")
	}
	for key, question := range input.Questions {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(question.Type) == "" || strings.TrimSpace(question.Instructions) == "" {
			return result, fmt.Errorf("invalid System One question %q", key)
		}
	}
	body, err := json.Marshal(struct {
		Model string `json:"model"`
		judgment.Request
	}{Model: c.model, Request: input})
	if err != nil {
		return result, fmt.Errorf("encode System One request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/systemone", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	resp, err := c.http.Do(req)
	if err != nil {
		return result, fmt.Errorf("call System One gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body)
	if err != nil {
		return result, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("System One gateway returned HTTP %d", resp.StatusCode)
	}
	result, err = decode(raw, input.Questions)
	return result, err
}

func decode(raw []byte, expected map[string]judgment.Question) (judgment.Result, error) {
	var envelope struct {
		Model   string                     `json:"model"`
		ID      string                     `json:"id"`
		Cost    string                     `json:"cost"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return judgment.Result{}, fmt.Errorf("decode System One response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return judgment.Result{}, fmt.Errorf("decode System One response: trailing JSON")
	}
	if len(envelope.Answers) != len(expected) {
		return judgment.Result{}, fmt.Errorf("System One answer set mismatch")
	}
	result := judgment.Result{Model: envelope.Model, Answers: make(map[string]judgment.Answer, len(expected))}
	for key, question := range expected {
		rawAnswer, ok := envelope.Answers[key]
		if !ok {
			return judgment.Result{}, fmt.Errorf("System One missing answer %q", key)
		}
		answer, err := decodeAnswer(rawAnswer, question.Type)
		if err != nil {
			return judgment.Result{}, fmt.Errorf("System One answer %q: %w", key, err)
		}
		result.Answers[key] = answer
	}
	for key := range envelope.Answers {
		if _, ok := expected[key]; !ok {
			return judgment.Result{}, fmt.Errorf("System One returned unexpected answer %q", key)
		}
	}
	return result, nil
}

func decodeAnswer(raw json.RawMessage, expectedType string) (judgment.Answer, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return judgment.Answer{}, fmt.Errorf("answer must be an object")
	}
	answer := judgment.Answer{Type: expectedType}
	for key := range fields {
		switch key {
		case "type", "choice", "value", "probability", "distribution", "score":
		default:
			return judgment.Answer{}, fmt.Errorf("unexpected field %q", key)
		}
	}
	if value, ok := fields["type"]; ok {
		if err := json.Unmarshal(value, &answer.Type); err != nil || answer.Type != expectedType {
			return judgment.Answer{}, fmt.Errorf("type mismatch")
		}
	}
	if rawChoice, ok := fields["choice"]; ok {
		if err := json.Unmarshal(rawChoice, &answer.Choice); err != nil || strings.TrimSpace(answer.Choice) == "" {
			return judgment.Answer{}, fmt.Errorf("invalid choice")
		}
	}
	if answer.Choice == "" {
		if rawValue, ok := fields["value"]; ok {
			_ = json.Unmarshal(rawValue, &answer.Choice)
		}
	}
	if rawProbability, ok := fields["probability"]; ok {
		if err := json.Unmarshal(rawProbability, &answer.Probability); err != nil || answer.Probability < 0 || answer.Probability > 1 {
			return judgment.Answer{}, fmt.Errorf("invalid probability")
		}
	}
	if rawDistribution, ok := fields["distribution"]; ok {
		if err := json.Unmarshal(rawDistribution, &answer.Distribution); err != nil {
			return judgment.Answer{}, fmt.Errorf("invalid distribution")
		}
		for choice, probability := range answer.Distribution {
			if strings.TrimSpace(choice) == "" || probability < 0 || probability > 1 {
				return judgment.Answer{}, fmt.Errorf("invalid distribution")
			}
		}
	}
	if rawBool, ok := fields["value"]; ok && expectedType == "noul" {
		if err := json.Unmarshal(rawBool, &answer.Bool); err != nil || answer.Bool == nil {
			return judgment.Answer{}, fmt.Errorf("invalid noul value")
		}
	}
	if rawScore, ok := fields["score"]; ok && expectedType == "score" {
		if err := json.Unmarshal(rawScore, &answer.Score); err != nil || answer.Score == nil || *answer.Score < 0 || *answer.Score > 1 {
			return judgment.Answer{}, fmt.Errorf("invalid score")
		}
	}
	switch expectedType {
	case "choice":
		if answer.Choice == "" || answer.Probability < 0 || answer.Probability > 1 {
			return judgment.Answer{}, fmt.Errorf("incomplete choice")
		}
	case "noul":
		_, hasProbability := fields["probability"]
		if answer.Bool == nil && !hasProbability && len(answer.Distribution) == 0 {
			return judgment.Answer{}, fmt.Errorf("incomplete noul")
		}
	case "score":
		if answer.Score == nil {
			return judgment.Answer{}, fmt.Errorf("incomplete score")
		}
	default:
		return judgment.Answer{}, fmt.Errorf("unsupported question type %q", expectedType)
	}
	return answer, nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read System One response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("System One response exceeds 2 MiB")
	}
	return raw, nil
}

func classify(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout") || strings.Contains(message, "deadline"):
		return "TIMEOUT"
	case strings.Contains(message, "http"):
		return "HTTP_ERROR"
	case strings.Contains(message, "decode") || strings.Contains(message, "invalid") || strings.Contains(message, "mismatch") || strings.Contains(message, "missing") || strings.Contains(message, "unexpected"):
		return "INVALID_RESPONSE"
	case strings.Contains(message, "not configured"):
		return "NOT_CONFIGURED"
	default:
		return "GATEWAY_ERROR"
	}
}
