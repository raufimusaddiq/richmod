package financialemail

import (
	"context"
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

func cashRuling(movement string) map[string]judgment.Answer {
	return map[string]judgment.Answer{
		"observation_type":        choice("CASH_MOVEMENT", judgment.ChoiceCriteria(observationTypeCriteria)),
		"movement_type":           choice(movement, judgment.ChoiceCriteria(movementTypeCriteria)),
		"cash_movement_supported": noul(0.99),
		"wealth_value_supported":  noul(0.02),
		"evidence_sufficient":     noul(0.99),
		"material_ambiguity":      noul(0.02),
	}
}

func cashObservation(confidence float64) observation {
	movement := "CONTRIBUTION"
	amount := "3000000"
	at := "2026-09-22T10:00:00+07:00"
	hint := "Jago"
	return observation{Kind: "CASH_MOVEMENT", MovementType: &movement, AmountIDR: &amount, OccurredAt: &at, FundingAccountHint: &hint, Confidence: confidence}
}

func stringPtrFor(value string) *string { return &value }

// The central behaviour change: a bounded ruling is sufficient even when the
// extractor's self-reported confidence is below the legacy 0.80 gate.
func TestCashMovementAllowedBelowLegacyConfidenceGate(t *testing.T) {
	verifier := &stubVerifier{answers: cashRuling("CONTRIBUTION")}
	processor := &Processor{verifier: verifier}
	classification, verified, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.35))
	if err != nil {
		t.Fatal(err)
	}
	if !verified || !classification.cashAllowed() {
		t.Fatalf("bounded support must authorize: verified=%v classification=%+v", verified, classification)
	}
	if classification.PolicyVersion != ProviderEmailClassificationPolicyVersion {
		t.Fatalf("policy version=%q", classification.PolicyVersion)
	}
	if _, ok := verifier.request.Questions["movement_type"]; !ok {
		t.Fatalf("cash observations must carry the movement question: %v", verifier.request.Questions)
	}
}

// A wealth value must never be classified as cash: money value is observed, not
// transacted (canonical invariant).
func TestWealthValueIsNotACashMovement(t *testing.T) {
	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"observation_type":        choice("WEALTH_VALUE", judgment.ChoiceCriteria(observationTypeCriteria)),
		"cash_movement_supported": noul(0.05),
		"wealth_value_supported":  noul(0.99),
		"evidence_sufficient":     noul(0.99),
		"material_ambiguity":      noul(0.02),
	}}
	processor := &Processor{verifier: verifier}
	classification, verified, err := processor.classifyObservation(context.Background(), "req", observation{Kind: "WEALTH_VALUE", ValueIDR: stringPtrFor("42700000"), Confidence: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || classification.cashAllowed() {
		t.Fatalf("a wealth value must not authorize a cash movement: %+v", classification)
	}
	if !classification.wealthAllowed() {
		t.Fatalf("a supported wealth value must be allowed: %+v", classification)
	}
}

func TestMovementTypeRequiredForCash(t *testing.T) {
	answers := cashRuling("CONTRIBUTION")
	delete(answers, "movement_type")
	processor := &Processor{verifier: &stubVerifier{answers: answers}}
	classification, _, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9))
	if err != nil {
		t.Fatal(err)
	}
	if classification.cashAllowed() {
		t.Fatalf("an undecided movement type must fail closed: %+v", classification)
	}
}

func TestInsufficientEvidenceFailsClosed(t *testing.T) {
	answers := cashRuling("WITHDRAWAL")
	answers["evidence_sufficient"] = noul(0.02)
	processor := &Processor{verifier: &stubVerifier{answers: answers}}
	classification, _, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9))
	if err != nil {
		t.Fatal(err)
	}
	if classification.cashAllowed() {
		t.Fatalf("insufficient evidence must fail closed: %+v", classification)
	}
}

func TestProviderFailureIsInfrastructureNotApproval(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, verified, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9)); err == nil || verified {
		t.Fatalf("provider failure must surface as an error, verified=%v err=%v", verified, err)
	}
}

// Unconfigured: no ruling is available, so callers keep their deterministic gate
// and must not read this as approval.
func TestUnconfiguredVerifierIsNotApproval(t *testing.T) {
	processor := &Processor{}
	if _, verified, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9)); err != nil || verified {
		t.Fatalf("unconfigured verifier must report unverified, verified=%v err=%v", verified, err)
	}
}
