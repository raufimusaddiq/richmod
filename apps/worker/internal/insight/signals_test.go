package insight

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

type stubVerifier struct {
	answers map[string]judgment.Answer
	err     error
	calls   int
	request judgment.Request
}

func (s *stubVerifier) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	s.calls++
	s.request = request
	if s.err != nil {
		return judgment.Result{}, s.err
	}
	return judgment.Result{Model: "stub-jev", Answers: s.answers}, nil
}

func noul(probability float64) judgment.Answer {
	return judgment.Answer{Type: "noul", Noul: probability, HasNoul: true}
}

func choice(label string, criteria map[string]any) judgment.Answer {
	answer := judgment.Answer{Type: "choice", Choice: label, HasConfidence: true, Confidence: 0.95, Distribution: map[string]float64{}}
	for key := range criteria {
		answer.Distribution[key] = 0.001
	}
	answer.Distribution[label] = 1 - 0.001*float64(len(criteria)-1)
	answer.Probability = answer.Distribution[label]
	return answer
}

func familyAnswer(label string) judgment.Answer {
	return choice(label, judgment.ChoiceCriteria(signalFamilyCriteria))
}

func facts(t *testing.T) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"income_idr": "10000000", "expense_idr": "5000000"})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// The whole point: ordinary numbers must not pay for prose generation.
func TestOrdinaryAggregatesSelectNoSignal(t *testing.T) {
	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"primary_signal_family":             familyAnswer("NONE"),
		"spending_change_material":          noul(0.02),
		"category_shift_material":           noul(0.02),
		"merchant_concentration_noteworthy": noul(0.02),
		"cashflow_pattern_noteworthy":       noul(0.02),
		"savings_pattern_noteworthy":        noul(0.02),
		"material_ambiguity":                noul(0.02),
	}}
	processor := &Processor{verifier: verifier}
	selection, selected, err := processor.selectSignal(context.Background(), "insight-1", facts(t))
	if err != nil {
		t.Fatal(err)
	}
	if !selected {
		t.Fatal("a configured verifier must report a ruling")
	}
	if selection.noteworthy() {
		t.Fatalf("ordinary aggregates must not request prose: %+v", selection)
	}
	if selection.PolicyVersion != InsightSignalPolicyVersion {
		t.Fatalf("policy version=%q", selection.PolicyVersion)
	}
	if verifier.calls != 1 {
		t.Fatalf("expected one bounded bundle, got %d", verifier.calls)
	}
}

func TestNoteworthyAggregateSelectsFamily(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{answers: map[string]judgment.Answer{
		"primary_signal_family":             familyAnswer("SPENDING_CHANGE"),
		"spending_change_material":          noul(0.97),
		"category_shift_material":           noul(0.05),
		"merchant_concentration_noteworthy": noul(0.05),
		"cashflow_pattern_noteworthy":       noul(0.05),
		"savings_pattern_noteworthy":        noul(0.05),
		"material_ambiguity":                noul(0.02),
	}}}
	selection, _, err := processor.selectSignal(context.Background(), "insight-2", facts(t))
	if err != nil {
		t.Fatal(err)
	}
	if !selection.noteworthy() || selection.Family != "SPENDING_CHANGE" {
		t.Fatalf("expected a noteworthy spending signal: %+v", selection)
	}
}

// A family with every claim undecided is not a signal: the undecided middle band
// must fail closed instead of narrating a guess.
func TestUndecidedClaimsAreNotNoteworthy(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{answers: map[string]judgment.Answer{
		"primary_signal_family":             familyAnswer("SPENDING_CHANGE"),
		"spending_change_material":          noul(0.50),
		"category_shift_material":           noul(0.50),
		"merchant_concentration_noteworthy": noul(0.50),
		"cashflow_pattern_noteworthy":       noul(0.50),
		"savings_pattern_noteworthy":        noul(0.50),
		"material_ambiguity":                noul(0.50),
	}}}
	selection, _, err := processor.selectSignal(context.Background(), "insight-3", facts(t))
	if err != nil {
		t.Fatal(err)
	}
	if selection.noteworthy() {
		t.Fatalf("undecided claims must not authorize prose: %+v", selection)
	}
}

// A high ambiguity claim blocks prose even when a family was named.
func TestAmbiguityBlocksNoteworthy(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{answers: map[string]judgment.Answer{
		"primary_signal_family":             familyAnswer("CATEGORY_SHIFT"),
		"spending_change_material":          noul(0.05),
		"category_shift_material":           noul(0.97),
		"merchant_concentration_noteworthy": noul(0.05),
		"cashflow_pattern_noteworthy":       noul(0.05),
		"savings_pattern_noteworthy":        noul(0.05),
		"material_ambiguity":                noul(0.95),
	}}}
	selection, _, err := processor.selectSignal(context.Background(), "insight-4", facts(t))
	if err != nil {
		t.Fatal(err)
	}
	if selection.noteworthy() {
		t.Fatalf("ambiguous aggregates must not authorize prose: %+v", selection)
	}
}

func TestSignalSelectionProviderFailureIsInfrastructure(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, selected, err := processor.selectSignal(context.Background(), "insight-5", facts(t)); err == nil || selected {
		t.Fatalf("provider failure must surface as an error, selected=%v err=%v", selected, err)
	}
}

func TestUnconfiguredSelectionIsNotApproval(t *testing.T) {
	processor := &Processor{}
	if _, selected, err := processor.selectSignal(context.Background(), "insight-6", facts(t)); err != nil || selected {
		t.Fatalf("unconfigured plane must report no ruling, selected=%v err=%v", selected, err)
	}
}
