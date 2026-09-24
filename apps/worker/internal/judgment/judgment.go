package judgment

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Question is the native System One question contract. Each primitive has its
// own criteria shape, so a wrong combination fails locally instead of reaching
// the provider.
type Question struct {
	Type         string
	Instructions string
	Criteria     any
}

// NoulCriteria describes both sides of a yes/no judgment.
type NoulCriteria struct {
	True  any `json:"true,omitempty"`
	False any `json:"false,omitempty"`
}

// ScoreCriteria is an ordered rubric. Score is not restricted to 0..1.
type ScoreCriteria []any

func (q Question) MarshalJSON() ([]byte, error) {
	if err := q.validate(); err != nil {
		return nil, err
	}
	payload := map[string]any{"type": q.Type, "instructions": q.Instructions}
	if q.Criteria != nil {
		payload["criteria"] = q.Criteria
	}
	return json.Marshal(payload)
}

func (q *Question) UnmarshalJSON(raw []byte) error {
	var payload struct {
		Type         string          `json:"type"`
		Instructions string          `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	q.Type, q.Instructions = payload.Type, payload.Instructions
	switch payload.Type {
	case "noul":
		if len(payload.Criteria) > 0 {
			var criteria NoulCriteria
			if err := json.Unmarshal(payload.Criteria, &criteria); err != nil {
				return fmt.Errorf("noul criteria: %w", err)
			}
			q.Criteria = criteria
		}
	case "choice":
		var criteria map[string]any
		if err := json.Unmarshal(payload.Criteria, &criteria); err != nil {
			return fmt.Errorf("choice criteria: %w", err)
		}
		q.Criteria = criteria
	case "score":
		var criteria ScoreCriteria
		if err := json.Unmarshal(payload.Criteria, &criteria); err != nil {
			return fmt.Errorf("score criteria: %w", err)
		}
		q.Criteria = criteria
	}
	return q.validate()
}

func (q Question) validate() error {
	if strings.TrimSpace(q.Instructions) == "" {
		return fmt.Errorf("question instructions are required")
	}
	switch q.Type {
	case "noul":
		if q.Criteria == nil {
			return nil
		}
		if _, ok := q.Criteria.(NoulCriteria); !ok {
			return fmt.Errorf("noul criteria must be true/false")
		}
	case "choice":
		criteria, ok := q.Criteria.(map[string]any)
		if !ok || len(criteria) < 2 {
			return fmt.Errorf("choice requires a criteria map with at least two options")
		}
		for label, description := range criteria {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("choice criteria option has an empty label")
			}
			if text, ok := description.(string); ok && strings.TrimSpace(text) == "" {
				return fmt.Errorf("choice criteria option %q has an empty description", label)
			}
		}
	case "score":
		criteria, ok := q.Criteria.(ScoreCriteria)
		if !ok || len(criteria) < 2 {
			return fmt.Errorf("score requires at least two ordered levels")
		}
	default:
		return fmt.Errorf("unsupported question type %q", q.Type)
	}
	return nil
}

// ChoiceCriteria builds a Choice criteria map from ordered labels.
func ChoiceCriteria(descriptions map[string]string) map[string]any {
	criteria := make(map[string]any, len(descriptions))
	for label, description := range descriptions {
		criteria[label] = description
	}
	return criteria
}

// CriteriaLabels returns the option labels of a criteria map in stable order.
func CriteriaLabels(criteria map[string]any) []string {
	labels := make([]string, 0, len(criteria))
	for label := range criteria {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// PlainCriteria accepts a bare label list and gives each option a neutral
// description so bounded server-state choices keep their native criteria map.
func PlainCriteria(labels []string) map[string]any {
	criteria := make(map[string]any, len(labels))
	for _, label := range labels {
		criteria[label] = "allowed option"
	}
	return criteria
}

// CategoryCriteria exposes the active category slugs plus an escape hatch.
func CategoryCriteria(active []string) map[string]any {
	criteria := make(map[string]any, len(active)+1)
	for _, slug := range active {
		criteria[slug] = "active expense category"
	}
	criteria["OTHER_OR_UNCLEAR"] = "no category is safe to choose"
	return criteria
}

type Request struct {
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// AnsweredDimensions narrows the per-question keys of a bounded result to the
// semantic facts the model actually returned. It is a fact about the request
// shape, never a policy acceptance decision, so phase telemetry can report what
// the model answered without claiming Go accepted it. Sorted for stable labels.
func AnsweredDimensions(questions map[string]Question, answers map[string]Answer) []string {
	keys := make([]string, 0, len(answers))
	for key := range answers {
		if _, ok := questions[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

type phaseContextKey struct{}
type sourceEventContextKey struct{}

type PhaseMetadata struct {
	Purpose       string
	PolicyVersion string
}

func WithPhaseMetadata(ctx context.Context, purpose, policyVersion string) context.Context {
	return context.WithValue(ctx, phaseContextKey{}, PhaseMetadata{Purpose: purpose, PolicyVersion: policyVersion})
}

func PhaseMetadataFromContext(ctx context.Context) PhaseMetadata {
	metadata, _ := ctx.Value(phaseContextKey{}).(PhaseMetadata)
	return metadata
}

func WithSourceEvent(ctx context.Context, sourceEventID string) context.Context {
	return context.WithValue(ctx, sourceEventContextKey{}, sourceEventID)
}

func SourceEventFromContext(ctx context.Context) string {
	sourceEventID, _ := ctx.Value(sourceEventContextKey{}).(string)
	return sourceEventID
}

type Answer struct {
	Type          string
	Choice        string
	Probability   float64
	Distribution  map[string]float64
	Confidence    float64
	HasConfidence bool
	Noul          float64
	HasNoul       bool
	Bool          *bool
	Score         *float64
	Legend        []string
}

type Result struct {
	Model   string
	Answers map[string]Answer
}

type Engine interface {
	Evaluate(context.Context, string, Request) (Result, error)
}

// ChoicePolicy versions the acceptance thresholds for one decision task.
type ChoicePolicy struct {
	MinTop        float64
	MinMargin     float64
	MinConfidence float64
}

// NoulPolicy maps a yes-probability into remember / do-not-remember /
// clarification.
type NoulPolicy struct {
	High float64
	Low  float64
}

// AcceptChoice requires a complete, server-consistent distribution. A missing
// distribution is never synthesized into an automatic decision.
func AcceptChoice(answer Answer, criteria map[string]any, policy ChoicePolicy) bool {
	if answer.Type != "choice" || answer.Choice == "" || len(criteria) < 2 {
		return false
	}
	if _, ok := criteria[answer.Choice]; !ok {
		return false
	}
	if len(answer.Distribution) != len(criteria) {
		return false
	}
	values := make([]float64, 0, len(answer.Distribution))
	total, top := 0.0, -1.0
	for label, probability := range answer.Distribution {
		if _, ok := criteria[label]; !ok || math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
			return false
		}
		if label == answer.Choice {
			top = probability
		}
		total += probability
		values = append(values, probability)
	}
	if top < 0 || math.Abs(total-1) > 1e-6 {
		return false
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(values)))
	if top < policy.MinTop || top != values[0] {
		return false
	}
	if len(values) > 1 && values[0]-values[1] < policy.MinMargin {
		return false
	}
	if policy.MinConfidence > 0 && (!answer.HasConfidence || answer.Confidence < policy.MinConfidence) {
		return false
	}
	return true
}

// AcceptNoul classifies a Noul into remember (true), do-not-remember (false),
// or an undecided middle band.
func AcceptNoul(answer Answer, policy NoulPolicy) (bool, bool) {
	if answer.Type != "noul" || !answer.HasNoul || math.IsNaN(answer.Noul) || math.IsInf(answer.Noul, 0) || answer.Noul < 0 || answer.Noul > 1 {
		return false, false
	}
	if answer.Noul >= policy.High {
		return true, true
	}
	if answer.Noul <= policy.Low {
		return false, true
	}
	return false, false
}
