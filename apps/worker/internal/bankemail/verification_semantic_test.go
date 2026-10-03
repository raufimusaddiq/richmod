package bankemail

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// The material semantic_grounded claim must accept ordinary Indonesian wording
// that clearly describes a spend, without requiring a literal channel token. The
// old channel_supported claim rejected 'kartu debit' because it could not match
// the token DEBIT_CARD, stamping a merchant-less debit-card notification
// UNKNOWN_BANK_TEMPLATE instead of flowing to the normal UNKNOWN_MERCHANT
// review; the material class ruling replaced that predicate.
func TestSemanticGroundedAcceptsIndonesianMethodWord(t *testing.T) {
	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"transaction_observed": noul(0.99),
		"amount_supported":     noul(0.99),
		"direction_supported":  noul(0.99),
		"semantic_grounded":    noul(0.99),
	}}
	processor := &Processor{verifier: verifier}
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: stringPtrFor("23600"), Direction: stringPtrFor("OUTGOING"), Channel: stringPtrFor("DEBIT_CARD"), Confidence: 0.99}
	verification, verified, err := processor.verifyEvidence(context.Background(), "src", extraction, TrustedEmail{
		Subject: "Transaksi kartu debit Jago", Body: "Transaksi menggunakan kartu debit Jago. Nominal: Rp23.600. Status: Berhasil.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || !verification.supported() {
		t.Fatalf("merchant-less debit card must pass verification: verified=%v v=%+v", verified, verification)
	}
}
