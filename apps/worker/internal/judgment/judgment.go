package judgment

import (
	"context"
	"sort"
)

type Question struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	Criteria     []string `json:"criteria,omitempty"`
}

type Request struct {
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type Answer struct {
	Type         string
	Choice       string
	Probability  float64
	Distribution map[string]float64
	Bool         *bool
	Score        *float64
}

type Result struct {
	Model   string
	Answers map[string]Answer
}

type Engine interface {
	Evaluate(context.Context, string, Request) (Result, error)
}

func AcceptChoice(answer Answer, minimumProbability, minimumMargin float64) bool {
	if answer.Type != "choice" || answer.Choice == "" || answer.Probability < minimumProbability || answer.Probability > 1 {
		return false
	}
	if len(answer.Distribution) < 2 {
		return true
	}
	values := make([]float64, 0, len(answer.Distribution))
	for _, probability := range answer.Distribution {
		values = append(values, probability)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(values)))
	return values[0]-values[1] >= minimumMargin
}
