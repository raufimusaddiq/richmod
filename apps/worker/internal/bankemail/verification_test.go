package bankemail

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// stubVerifier answers the verification bundle from a fixed ruling, and can
// simulate a provider outage.
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

func supportedRuling() map[string]judgment.Answer {
	return map[string]judgment.Answer{
		"transaction_observed": noul(0.99),
		"amount_supported":     noul(0.99),
		"direction_supported":  noul(0.99),
		"channel_supported":    noul(0.99),
		"material_ambiguity":   noul(0.02),
	}
}

func choice(label string, criteria map[string]any) judgment.Answer {
	answer := judgment.Answer{Type: "choice", Choice: label, HasConfidence: true, Confidence: 0.99, Distribution: map[string]float64{}}
	for key := range criteria {
		answer.Distribution[key] = 0.001
	}
	answer.Distribution[label] = 1 - 0.001*float64(len(criteria)-1)
	answer.Probability = answer.Distribution[label]
	return answer
}

func stringPtrFor(value string) *string { return &value }

func testExtraction() Extraction {
	return Extraction{Kind: "TRANSACTION", AmountIDR: stringPtrFor("25000"), Direction: stringPtrFor("OUTGOING"), Channel: stringPtrFor("QR"), Merchant: stringPtrFor("Toko"), Confidence: 0.55}
}

// The whole point of the bundle: a fully supported ruling is accepted even when
// the extractor's own confidence is far below the legacy 0.80 gate (PRD §20).
func TestEvidenceVerificationSupportsLowExtractorConfidence(t *testing.T) {
	verifier := &stubVerifier{answers: supportedRuling()}
	processor := &Processor{verifier: verifier}
	verification, verified, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{Subject: "QR", Body: "bayar 25000"})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || !verification.supported() {
		t.Fatalf("supported ruling must be usable: verified=%v verification=%+v", verified, verification)
	}
	if verification.PolicyVersion != BankEmailVerificationPolicyVersion {
		t.Fatalf("policy version=%q", verification.PolicyVersion)
	}
	if verifier.calls != 1 {
		t.Fatalf("expected one bounded bundle, got %d", verifier.calls)
	}
	// Every claim must ride in the same request: one snapshot, one round trip.
	for _, claim := range []string{"transaction_observed", "amount_supported", "direction_supported", "channel_supported", "material_ambiguity"} {
		if _, ok := verifier.request.Questions[claim]; !ok {
			t.Fatalf("missing claim %q in %v", claim, verifier.request.Questions)
		}
	}
}

func TestEvidenceVerificationRejectsUnsupportedClaim(t *testing.T) {
	answers := supportedRuling()
	answers["channel_supported"] = noul(0.30)
	processor := &Processor{verifier: &stubVerifier{answers: answers}}
	verification, verified, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || verification.supported() {
		t.Fatalf("undecided claim must fail closed: %+v", verification)
	}
}

// A high self-reported ambiguity must block even when every other claim is fine,
// which is the same fail-closed direction the Telegram plane uses.
func TestEvidenceVerificationRejectsMaterialAmbiguity(t *testing.T) {
	answers := supportedRuling()
	answers["material_ambiguity"] = noul(0.95)
	processor := &Processor{verifier: &stubVerifier{answers: answers}}
	verification, _, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verification.MaterialAmbiguity || verification.supported() {
		t.Fatalf("ambiguous evidence must not be supported: %+v", verification)
	}
}

// A missing answer is never silently treated as a positive claim.
func TestEvidenceVerificationMissingAnswersFailClosed(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{answers: map[string]judgment.Answer{}}}
	verification, verified, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || verification.supported() {
		t.Fatalf("missing answers must not authorize: %+v", verification)
	}
}

func TestEvidenceVerificationProviderFailureIsInfrastructure(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, verified, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{}); err == nil || verified {
		t.Fatalf("provider failure must surface as an error, verified=%v err=%v", verified, err)
	}
}

// With no configured verifier the caller keeps the deterministic structural gate
// and must never read this as "verified" approval.
func TestEvidenceVerificationUnconfiguredIsNotApproval(t *testing.T) {
	processor := &Processor{}
	if _, verified, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{}); err != nil || verified {
		t.Fatalf("unconfigured verifier must report unverified, verified=%v err=%v", verified, err)
	}
}

// Regression: the ambiguity claim is inverted, so a decided negative is the
// favourable answer. An undecided middle-band answer to "is this ambiguous?"
// must NOT be read as "not ambiguous" — that is fail-open, and it is what let a
// genuinely ambiguous email auto-confirm (Hermes review of the PRD 25 tests).
func TestEvidenceVerificationUndecidedAmbiguityFailsClosed(t *testing.T) {
	// 0.10 sits between Low 0.05 and High 0.15: the plane could not tell.
	answers := supportedRuling()
	answers["material_ambiguity"] = noul(0.10)
	processor := &Processor{verifier: &stubVerifier{answers: answers}}
	verification, verified, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("the bundle was answered, so it is verified")
	}
	if verification.MaterialAmbiguity {
		t.Fatalf("an undecided answer is not an affirmative ambiguous ruling: %+v", verification)
	}
	if verification.AmbiguityDecidedNotAmbiguous {
		t.Fatalf("an undecided answer must not be recorded as decided-not-ambiguous: %+v", verification)
	}
	if verification.supported() {
		t.Fatalf("an undecided ambiguity ruling must fail closed, not authorize: %+v", verification)
	}
}

// The other half: a decided "not ambiguous" (at or below Low) is what clears it.
func TestEvidenceVerificationDecidedNotAmbiguousAuthorizes(t *testing.T) {
	verification, verified, err := (&Processor{verifier: &stubVerifier{answers: supportedRuling()}}).verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || !verification.AmbiguityDecidedNotAmbiguous || !verification.supported() {
		t.Fatalf("a decided not-ambiguous ruling must authorize: %+v", verification)
	}
}
