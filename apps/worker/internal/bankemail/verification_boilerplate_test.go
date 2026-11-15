package bankemail

import (
	"context"
	"strings"
	"testing"
)

// Protective footers ("Jika kamu tidak melakukan transaksi ini...") once made a real Jago email unsupported.
// Pin that such footers make neither a completed transaction unreal nor a competing amount.
func TestVerificationClaimsTreatSecurityBoilerplateAsNeutral(t *testing.T) {
	verifier := &stubVerifier{answers: supportedRuling()}
	processor := &Processor{verifier: verifier}
	if _, _, err := processor.verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{Subject: "kartu debit Jago", Body: "transaksi sebesar Rp53.000"}); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		claim  string
		needle string
	}{
		{"transaction_observed", "lock your card"},
		{"amount_supported", "phone number"},
	} {
		question, ok := verifier.request.Questions[testCase.claim]
		if !ok {
			t.Fatalf("missing claim %q", testCase.claim)
		}
		if !strings.Contains(strings.ToLower(question.Instructions), testCase.needle) {
			t.Fatalf("claim %q no longer tells the provider that %q is neutral: %s", testCase.claim, testCase.needle, question.Instructions)
		}
	}
}
