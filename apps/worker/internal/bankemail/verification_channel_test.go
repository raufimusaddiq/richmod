package bankemail

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// The channel_supported claim must accept ordinary wording that names the
// payment method, not require a literal vocabulary token. Before this wording
// change the verifier rejected essentially every Indonesian bank email because
// 'kartu debit' could not match the token DEBIT_CARD, causing a merchant-less
// debit-card notification to be stamped UNKNOWN_BANK_TEMPLATE instead of
// flowing through to the normal UNKNOWN_MERCHANT review.
func TestChannelSupportedAcceptsIndonesianMethodWord(t *testing.T) {
	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"transaction_observed": noul(0.99),
		"amount_supported":     noul(0.99),
		"direction_supported":  noul(0.99),
		"channel_supported":    noul(0.99),
		"material_ambiguity":   noul(0.02),
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
