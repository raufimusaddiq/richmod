package bankemail

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// semantic_grounded must accept clear Indonesian spend wording; the old channel_supported rejected
// "kartu debit" and stamped UNKNOWN_BANK_TEMPLATE instead of reaching UNKNOWN_MERCHANT review.
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
