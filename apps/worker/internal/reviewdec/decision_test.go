package reviewdec

import "testing"

// Every active reason must encode: a producer that falls back to its preset can
// never be refused by Validate or by the review_item decision trigger.
func TestEveryActivePresetValidates(t *testing.T) {
	for _, reason := range ActiveReasons() {
		decision, ok := Preset(reason, "transaction", "00000000-0000-0000-0000-000000000000")
		if !ok {
			t.Fatalf("%s: no preset", reason)
		}
		if _, err := decision.JSON(); err != nil {
			t.Fatalf("%s: preset does not encode: %v", reason, err)
		}
	}
}

func TestJSONRefusesIncompleteDecision(t *testing.T) {
	for name, decision := range map[string]Decision{
		"zero":               {},
		"no reason":          {AllowedActions: []string{"IGNORE"}},
		"no allowed actions": {ReasonCode: "AMBIGUOUS_CATEGORY"},
		"empty actions":      {ReasonCode: "AMBIGUOUS_CATEGORY", AllowedActions: []string{}},
	} {
		if err := decision.Validate(); err == nil {
			t.Fatalf("%s: Validate accepted an incomplete decision", name)
		}
		if encoded, err := decision.JSON(); err == nil {
			t.Fatalf("%s: JSON encoded an incomplete decision: %s", name, encoded)
		}
	}
}
