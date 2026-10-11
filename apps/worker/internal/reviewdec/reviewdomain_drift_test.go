package reviewdec

import (
	"slices"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// TestReopenDuplicateDecisionMatchesPreset pins reviewdomain's hand-copied
// POSSIBLE_DUPLICATE contract (used when Unmerge reopens a review) to the preset
// every other producer uses, so a reopened review cannot advertise different
// actions or render differently from a freshly created one.
func TestReopenDuplicateDecisionMatchesPreset(t *testing.T) {
	preset, ok := Preset("POSSIBLE_DUPLICATE", "transaction", "00000000-0000-0000-0000-000000000000")
	if !ok {
		t.Fatal("POSSIBLE_DUPLICATE has no preset")
	}
	reopen := reviewdomain.PossibleDuplicateDecisionContract()
	if reopen.ReasonCode != preset.ReasonCode {
		t.Fatalf("reasonCode drifted: reopen=%q preset=%q", reopen.ReasonCode, preset.ReasonCode)
	}
	if reopen.DecisionClass != preset.DecisionClass {
		t.Fatalf("decisionClass drifted: reopen=%q preset=%q", reopen.DecisionClass, preset.DecisionClass)
	}
	if reopen.InteractionMode != preset.InteractionMode {
		t.Fatalf("interactionMode drifted: reopen=%q preset=%q", reopen.InteractionMode, preset.InteractionMode)
	}
	got, want := slices.Sorted(slices.Values(reopen.AllowedActions)), slices.Sorted(slices.Values(preset.AllowedActions))
	if !slices.Equal(got, want) {
		t.Fatalf("allowedActions drifted: reopen=%v preset=%v", got, want)
	}
}
