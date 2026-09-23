package bankemail

import (
	"context"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// EvidenceVerification is the bounded ruling over one already-extracted bank
// notification. It is the bank-email analogue of the Telegram transaction
// decision: extraction produced arbitrary facts, this object decides whether
// those facts are safe to hand to the deterministic bank policy (PRD §20).
type EvidenceVerification struct {
	TransactionObserved bool
	AmountSupported     bool
	DirectionSupported  bool
	ChannelSupported    bool
	MaterialAmbiguity   bool

	Model         string
	PolicyVersion string
}

// supported reports whether every bounded claim was decided in the extractor's
// favour. Anything undecided or affirmative-ambiguous fails closed.
func (v EvidenceVerification) supported() bool {
	return v.TransactionObserved && v.AmountSupported && v.DirectionSupported && v.ChannelSupported && !v.MaterialAmbiguity
}

// jeverifier is the seam onto the bounded judgment plane. It is defined here, in
// the terms this package needs, so bank email never imports Telegram policy.
type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

// BankEmailVerificationPolicyVersion marks the thresholds that ruled on these
// verifications, so a stored decision stays reproducible (PRD §18).
const BankEmailVerificationPolicyVersion = "2026-09-jev2"

// evidenceVerificationPolicy is the bank-email slice of the shared threshold
// policy. A Noul here answers "does the email itself support this claim?".
var evidenceVerificationPolicy = struct {
	Amount    judgment.NoulPolicy
	Direction judgment.NoulPolicy
	Channel   judgment.NoulPolicy
	Observed  judgment.NoulPolicy
	Ambiguity judgment.NoulPolicy
}{
	Amount:    judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Direction: judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Channel:   judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Observed:  judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Ambiguity: judgment.NoulPolicy{High: 0.15, Low: 0.05},
}

// verificationClaims is the bounded question set. Each claim is about facts the
// deterministic bank policy will act on, and each is answered from the same
// minimized state snapshot (PRD §20).
var verificationClaims = []struct {
	Key          string
	Instructions string
	Policy       judgment.NoulPolicy
}{
	{"transaction_observed", "Does the email describe one real completed transaction rather than a promotion, statement, or unrelated notice?", evidenceVerificationPolicy.Observed},
	{"amount_supported", "Is the extracted amount exactly what the email states, with no other plausible amount in the message?", evidenceVerificationPolicy.Amount},
	{"direction_supported", "Does the email's wording support the extracted money direction (INCOMING or OUTGOING)? Answer yes when ordinary wording implies it, for example a debit-card or payment notification for OUTGOING and a transfer-received notice for INCOMING. Answer no only when the email suggests the opposite direction or none at all.", evidenceVerificationPolicy.Direction},
	{"channel_supported", "Does the email describe the same payment method as the extracted channel? The channel is a server vocabulary token (DEBIT_CARD, MERCHANT_PAYMENT, QR, TRANSFER, ATM, BANK_FEE, INTERNAL_TRANSFER, RDN, OTHER), so ordinary wording that names that method counts, for example 'kartu debit' or 'debit card' for DEBIT_CARD, 'QR' for QR, 'transfer' for TRANSFER. Answer no only when the email names a different method or none.", evidenceVerificationPolicy.Channel},
	{"material_ambiguity", "Is this notification genuinely ambiguous, for example two plausible amounts, dates, or targets?", evidenceVerificationPolicy.Ambiguity},
}

// verifyEvidence asks one bounded bundle about an already-extracted bank email.
// The provider never produces merchant or date strings here; it only rules on
// claims Go already holds. A provider failure is returned as an error so the
// caller can take the safe retry/review path instead of trusting confidence.
func (p *Processor) verifyEvidence(ctx context.Context, sourceEventID string, extraction Extraction, email TrustedEmail) (EvidenceVerification, bool, error) {
	if p.verifier == nil {
		return EvidenceVerification{}, false, nil
	}
	questions := make(map[string]judgment.Question, len(verificationClaims))
	for _, claim := range verificationClaims {
		questions[claim.Key] = judgment.Question{Type: "noul", Instructions: claim.Instructions}
	}
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-verify", judgment.Request{
		State: map[string]any{
			"email_subject": email.Subject,
			"email_body":    "<untrusted_email_body>" + normalizeVisibleText(email.Body) + "</untrusted_email_body>",
			"extracted": map[string]any{
				"kind":           extraction.Kind,
				"amount_idr":     pointerValue(extraction.AmountIDR),
				"direction":      pointerValue(extraction.Direction),
				"channel":        pointerValue(extraction.Channel),
				"merchant":       pointerValue(extraction.Merchant),
				"transaction_at": timeValue(extraction.TransactionAt),
			},
		},
		Questions: questions,
	})
	if err != nil {
		// No ruling was obtained. The caller must treat this as infrastructure
		// failure, never as a verified (or implicitly approved) verdict.
		return EvidenceVerification{}, false, err
	}
	verification := EvidenceVerification{Model: result.Model, PolicyVersion: BankEmailVerificationPolicyVersion}
	verification.TransactionObserved = noulClaimed(result.Answers, "transaction_observed", evidenceVerificationPolicy.Observed)
	verification.AmountSupported = noulClaimed(result.Answers, "amount_supported", evidenceVerificationPolicy.Amount)
	verification.DirectionSupported = noulClaimed(result.Answers, "direction_supported", evidenceVerificationPolicy.Direction)
	verification.ChannelSupported = noulClaimed(result.Answers, "channel_supported", evidenceVerificationPolicy.Channel)
	verification.MaterialAmbiguity = noulClaimed(result.Answers, "material_ambiguity", evidenceVerificationPolicy.Ambiguity)
	return verification, true, nil
}

// noulClaimed reports a decided, affirmative Noul. The undecided middle band
// fails closed, exactly like the Telegram decision plane.
func noulClaimed(answers map[string]judgment.Answer, key string, policy judgment.NoulPolicy) bool {
	answer, ok := answers[key]
	if !ok {
		return false
	}
	claimed, decided := judgment.AcceptNoul(answer, policy)
	return claimed && decided
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func timeValue(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
