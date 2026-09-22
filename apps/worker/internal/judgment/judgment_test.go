package judgment

import (
	"encoding/json"
	"strings"
	"testing"
)

var routeCriteria = map[string]any{"READ_WEALTH": "net worth", "OTHER_OR_UNCLEAR": "no safe route"}

func strongChoice() Answer {
	distribution := map[string]float64{"READ_WEALTH": 0.95, "OTHER_OR_UNCLEAR": 0.05}
	return Answer{Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, Distribution: distribution, Confidence: 0.93, HasConfidence: true}
}

func TestAcceptChoiceRequiresCompleteConsistentDistribution(t *testing.T) {
	policy := ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.80}
	if !AcceptChoice(strongChoice(), routeCriteria, policy) {
		t.Fatal("strong complete choice should be accepted")
	}
	for name, answer := range map[string]Answer{
		"missing distribution": {Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, HasConfidence: true, Confidence: 0.93},
		"partial distribution": {Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, Distribution: map[string]float64{"READ_WEALTH": 0.95}, HasConfidence: true, Confidence: 0.93},
		"unexpected option":    {Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, Distribution: map[string]float64{"READ_WEALTH": 0.95, "OTHER_OR_UNCLEAR": 0.04, "X": 0.01}, HasConfidence: true, Confidence: 0.93},
		"not summing to one":   {Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, Distribution: map[string]float64{"READ_WEALTH": 0.95, "OTHER_OR_UNCLEAR": 0.03}, HasConfidence: true, Confidence: 0.93},
		"low confidence":       {Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, Distribution: map[string]float64{"READ_WEALTH": 0.95, "OTHER_OR_UNCLEAR": 0.05}, HasConfidence: true, Confidence: 0.50},
		"missing confidence":   {Type: "choice", Choice: "READ_WEALTH", Probability: 0.95, Distribution: map[string]float64{"READ_WEALTH": 0.95, "OTHER_OR_UNCLEAR": 0.05}},
	} {
		t.Run(name, func(t *testing.T) {
			if AcceptChoice(answer, routeCriteria, policy) {
				t.Fatalf("%s must be rejected", name)
			}
		})
	}
	notArgmax := strongChoice()
	notArgmax.Choice = "OTHER_OR_UNCLEAR"
	if AcceptChoice(notArgmax, routeCriteria, policy) {
		t.Fatal("non-argmax selection must be rejected")
	}
}

func TestChoiceQuestionSerializesCriteriaMap(t *testing.T) {
	question := Question{Type: "choice", Instructions: "pick", Criteria: routeCriteria}
	raw, err := json.Marshal(question)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Criteria map[string]any `json:"criteria"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Criteria["READ_WEALTH"] != "net worth" {
		t.Fatalf("criteria map missing: %s", raw)
	}
	if _, err := json.Marshal(Question{Type: "choice", Instructions: "pick"}); err == nil {
		t.Fatal("choice without criteria must fail locally")
	}
	if _, err := json.Marshal(Question{Type: "noul", Instructions: "pick", Criteria: routeCriteria}); err == nil {
		t.Fatal("noul with choice criteria must fail locally")
	}
	if _, err := json.Marshal(Question{Type: "score", Instructions: "pick", Criteria: ScoreCriteria{"low", "high"}}); err != nil {
		t.Fatal("score with ordered levels should serialize")
	}
	if _, err := json.Marshal(Question{Type: "unknown", Instructions: "pick"}); err == nil {
		t.Fatal("unknown primitive must fail locally")
	}
}

func TestNoulQuestionAndPolicy(t *testing.T) {
	raw, err := json.Marshal(Question{Type: "noul", Instructions: "consent?", Criteria: NoulCriteria{True: "explicit yes", False: "otherwise"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"true":"explicit yes"`) {
		t.Fatalf("noul criteria missing: %s", raw)
	}
	policy := NoulPolicy{High: 0.80, Low: 0.20}
	if remember, decided := AcceptNoul(Answer{Type: "noul", Noul: 0.91, HasNoul: true}, policy); !remember || !decided {
		t.Fatal("high noul should remember")
	}
	if remember, decided := AcceptNoul(Answer{Type: "noul", Noul: 0.05, HasNoul: true}, policy); remember || !decided {
		t.Fatal("low noul should not remember")
	}
	if _, decided := AcceptNoul(Answer{Type: "noul", Noul: 0.5, HasNoul: true}, policy); decided {
		t.Fatal("middle noul should ask clarification")
	}
	if _, decided := AcceptNoul(Answer{Type: "noul", Noul: 5, HasNoul: true}, policy); decided {
		t.Fatal("out-of-range noul must be rejected")
	}
}
