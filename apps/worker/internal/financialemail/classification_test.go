package financialemail

import (
	"context"
	"errors"
	"strings"
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
	if verifier.calls != 1 {
		t.Fatalf("material checks must use one existing bounded pass, got %d", verifier.calls)
	}
	if classification.PolicyVersion != ProviderEmailClassificationPolicyVersion {
		t.Fatalf("policy version=%q", classification.PolicyVersion)
	}
	if _, ok := verifier.request.Questions["movement_type"]; !ok {
		t.Fatalf("cash observations must carry the movement question: %v", verifier.request.Questions)
	}
	if question := verifier.request.Questions["evidence_sufficient"].Instructions; strings.Contains(question, "hints fully supported") || !strings.Contains(question, "Do not require account hints") {
		t.Fatalf("account hint must not be an evidence gate: %q", question)
	}
}

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

func TestUndecidedAmbiguityFailsClosed(t *testing.T) {
	for _, probability := range []float64{0.02, 0.10} {
		answers := cashRuling("CONTRIBUTION")
		answers["material_ambiguity"] = noul(probability)
		processor := &Processor{verifier: &stubVerifier{answers: answers}}
		classification, _, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9))
		if err != nil {
			t.Fatal(err)
		}
		if probability < classificationPolicy.Ambiguity.Low && !classification.cashAllowed() {
			t.Fatalf("a decided not-ambiguous ruling must authorize: %+v", classification)
		}
		if probability >= classificationPolicy.Ambiguity.Low && classification.cashAllowed() {
			t.Fatalf("undecided ambiguity (noul=%.2f) must fail closed: %+v", probability, classification)
		}
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

func TestProviderFailureRemainsInfrastructure(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	plan, err := processor.planCash(context.Background(), nil, "household", "", "", "", false, "", "", cashObservation(0.9))
	if err == nil || plan.review != "" {
		t.Fatalf("provider outage must retry without a human review: plan=%+v err=%v", plan, err)
	}
}

func TestUnconfiguredVerifierIsNotApproval(t *testing.T) {
	processor := &Processor{}
	if _, verified, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9)); err != nil || verified {
		t.Fatalf("unconfigured verifier must report unverified, verified=%v err=%v", verified, err)
	}
}

func TestAmbiguityUsesTheInvertedPolicyConsistently(t *testing.T) {
	for _, probability := range []float64{0.10, 0.20} {
		answers := cashRuling("CONTRIBUTION")
		answers["material_ambiguity"] = noul(probability)
		processor := &Processor{verifier: &stubVerifier{answers: answers}}
		classification, _, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9))
		if err != nil {
			t.Fatal(err)
		}
		if classification.cashAllowed() {
			t.Fatalf("undecided ambiguity (noul=%.2f) must fail closed: %+v", probability, classification)
		}
		if !containsString(classification.cashResidual(), "transaction_ambiguity") {
			t.Fatalf("undecided ambiguity (noul=%.2f) must produce the transaction_ambiguity residual: %+v", probability, classification.cashResidual())
		}
	}
}

func TestRejectedChoiceOutcomeIsUndecidedNotPassing(t *testing.T) {
	answers := cashRuling("CONTRIBUTION")
	marginal := choice("CASH_MOVEMENT", judgment.ChoiceCriteria(observationTypeCriteria))
	marginal.Confidence = 0.50
	answers["observation_type"] = marginal
	processor := &Processor{verifier: &stubVerifier{answers: answers}}
	classification, verified, err := processor.classifyObservation(context.Background(), "req", cashObservation(0.9))
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("the plane answered, so the ruling must be verified")
	}
	if classification.TypeAccepted || classification.cashAllowed() {
		t.Fatalf("a rejected choice must not authorise: %+v", classification)
	}
	if classification.ClaimOutcomes["observation_type"] != "UNDECIDED" {
		t.Fatalf("a rejected choice must record UNDECIDED, got %q", classification.ClaimOutcomes["observation_type"])
	}
	if !containsString(classification.cashResidual(), "observation_type") {
		t.Fatalf("a rejected observation_type must reach the residual, got %+v", classification.cashResidual())
	}
}
