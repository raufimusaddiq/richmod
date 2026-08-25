package insight

import (
	"strings"
	"testing"
)

func TestCompletenessThreshold(t *testing.T) {
	if !belowThreshold("0.6999", "0.7000") || belowThreshold("0.7000", "0.7000") {
		t.Fatal("unexpected completeness threshold")
	}
}

func TestValidateOutput(t *testing.T) {
	text, confidence, err := validateOutput(output{Summary: "Arus kas positif.", Observations: []observation{{Title: "Makan", Detail: "Naik dibanding rata-rata."}}, Confidence: .82})
	if err != nil || confidence != .82 || !strings.Contains(text, "• Makan") {
		t.Fatalf("unexpected output: %q %v %v", text, confidence, err)
	}
}
