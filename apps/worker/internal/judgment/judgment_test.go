package judgment

import "testing"

func TestAcceptChoiceRequiresProbabilityAndMargin(t *testing.T) {
	if !AcceptChoice(Answer{Type: "choice", Choice: "READ_WEALTH", Probability: 0.9, Distribution: map[string]float64{"READ_WEALTH": 0.9, "OTHER_OR_UNCLEAR": 0.1}}, 0.85, 0.2) {
		t.Fatal("strong choice should be accepted")
	}
	if AcceptChoice(Answer{Type: "choice", Choice: "READ_WEALTH", Probability: 0.9, Distribution: map[string]float64{"READ_WEALTH": 0.9, "OTHER_OR_UNCLEAR": 0.8}}, 0.85, 0.2) {
		t.Fatal("narrow choice should be rejected")
	}
}
