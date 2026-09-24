package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

const maxResponseBytes = 2 << 20

type Metric struct {
	SourceEventID      string
	Purpose            string
	PolicyVersion      string
	Dimensions         []string
	AnsweredDimensions []string
	Model              string
	Status             string
	ErrorClass         string
	DurationMs         int64
	QuestionCount      int
}

type Recorder func(context.Context, Metric)

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
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

func (c *Client) Evaluate(ctx context.Context, requestID string, input judgment.Request) (result judgment.Result, err error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return result, fmt.Errorf("System One gateway is not configured")
	}
	if len(input.Questions) == 0 {
		return result, fmt.Errorf("System One request has no questions")
	}
	for key, question := range input.Questions {
		if strings.TrimSpace(key) == "" {
			return result, fmt.Errorf("invalid System One question %q", key)
		}
		if _, err = json.Marshal(question); err != nil {
			return result, fmt.Errorf("invalid System One question %q: %w", key, err)
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
	allowed := map[string]bool{"type": true}
	switch expectedType {
	case "choice":
		allowed["choice"], allowed["probabilities"], allowed["confidence"] = true, true, true
	case "noul":
		allowed["noul"] = true
	case "score":
		allowed["score"], allowed["legend"], allowed["probabilities"], allowed["confidence"] = true, true, true, true
	default:
		return judgment.Answer{}, fmt.Errorf("unsupported question type %q", expectedType)
	}
	for key := range fields {
		if !allowed[key] {
			return judgment.Answer{}, fmt.Errorf("unexpected field %q", key)
		}
	}
	answer := judgment.Answer{Type: expectedType}
	if value, ok := fields["type"]; ok {
		if err := json.Unmarshal(value, &answer.Type); err != nil || answer.Type != expectedType {
			return judgment.Answer{}, fmt.Errorf("type mismatch")
		}
	}
	switch expectedType {
	case "choice":
		if err := json.Unmarshal(fields["choice"], &answer.Choice); err != nil || strings.TrimSpace(answer.Choice) == "" {
			return judgment.Answer{}, fmt.Errorf("invalid choice")
		}
		probabilities, err := decodeProbabilities(fields["probabilities"], true)
		if err != nil {
			return judgment.Answer{}, err
		}
		answer.Distribution = probabilities
		top, ok := probabilities[answer.Choice]
		if !ok {
			return judgment.Answer{}, fmt.Errorf("choice %q missing from probabilities", answer.Choice)
		}
		answer.Probability = top
		if answer.HasConfidence, err = decodeConfidence(&answer, fields["confidence"]); err != nil {
			return judgment.Answer{}, err
		}
	case "noul":
		if err := json.Unmarshal(fields["noul"], &answer.Noul); err != nil || answer.Noul < 0 || answer.Noul > 1 {
			return judgment.Answer{}, fmt.Errorf("invalid noul")
		}
		answer.HasNoul = true
	case "score":
		if err := json.Unmarshal(fields["score"], &answer.Score); err != nil || answer.Score == nil {
			return judgment.Answer{}, fmt.Errorf("invalid score")
		}
		if rawLegend, ok := fields["legend"]; ok {
			if err := json.Unmarshal(rawLegend, &answer.Legend); err != nil {
				return judgment.Answer{}, fmt.Errorf("invalid legend")
			}
		}
		probabilities, err := decodeProbabilities(fields["probabilities"], len(fields["probabilities"]) > 0)
		if err != nil {
			return judgment.Answer{}, err
		}
		answer.Distribution = probabilities
		if answer.HasConfidence, err = decodeConfidence(&answer, fields["confidence"]); err != nil {
			return judgment.Answer{}, err
		}
	default:
		return judgment.Answer{}, fmt.Errorf("unsupported question type %q", expectedType)
	}
	return answer, nil
}

func decodeProbabilities(raw json.RawMessage, required bool) (map[string]float64, error) {
	if len(raw) == 0 {
		if required {
			return nil, fmt.Errorf("missing probabilities")
		}
		return nil, nil
	}
	var rawProbabilities map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawProbabilities); err != nil {
		return nil, fmt.Errorf("invalid probabilities")
	}
	if rawProbabilities == nil {
		return nil, fmt.Errorf("invalid probabilities")
	}
	probabilities := make(map[string]float64, len(rawProbabilities))
	for label, rawProbability := range rawProbabilities {
		if bytes.Equal(bytes.TrimSpace(rawProbability), []byte("null")) {
			return nil, fmt.Errorf("invalid probabilities")
		}
		var probability float64
		if err := json.Unmarshal(rawProbability, &probability); err != nil {
			return nil, fmt.Errorf("invalid probabilities")
		}
		probabilities[label] = probability
	}
	if len(probabilities) == 0 {
		return nil, fmt.Errorf("empty probabilities")
	}
	for label, probability := range probabilities {
		if strings.TrimSpace(label) == "" || math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
			return nil, fmt.Errorf("invalid probabilities")
		}
	}
	return probabilities, nil
}

func decodeConfidence(answer *judgment.Answer, raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, &answer.Confidence); err != nil || math.IsNaN(answer.Confidence) || math.IsInf(answer.Confidence, 0) || answer.Confidence < 0 || answer.Confidence > 1 {
		return false, fmt.Errorf("invalid confidence")
	}
	return true, nil
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
