package bankemail

import (
	"context"
	"strings"
	"testing"
)

// Indonesian bank notifications always close with protective boilerplate such as
// "Jika kamu tidak melakukan transaksi ini, silakan lihat cara Kunci Kartu Debit".
// A real Jago transaction email carrying that footer was judged unsupported on
// transaction_observed (and, on a second sample, on amount_supported and
// material_ambiguity), so a completed debit-card transaction was stamped
// UNKNOWN_BANK_TEMPLATE instead of reaching the normal merchant review.
//
// The claim wording must state that routine security/support footers do not make
// a completed transaction unreal, a competing amount, or ambiguous. Pin that
// guidance so a future edit cannot silently reintroduce the rejection.
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
		{"material_ambiguity", "boilerplate"},
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
