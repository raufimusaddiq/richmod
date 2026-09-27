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
	// SemanticGrounded rules that the email's wording supports the canonical
	// class the deterministic policy will act on (an ordinary spend versus a
	// transfer/internal movement). It replaces channel_supported: the exact
	// payment mechanism (QR vs DEBIT_CARD vs MERCHANT_PAYMENT) is evidence
	// metadata, not a required human fact, so an uncertain mechanism must not
	// independently block an otherwise safe expense (SAVR-06).
	SemanticGrounded  bool
	MaterialAmbiguity bool

	// ClaimOutcomes keeps YES/NO/UNDECIDED per bounded predicate so a review can
	// name the exact material predicate that did not clear, independently of the
	// booleans used by the canonical auto-confirm guard (SAVR-06).
	ClaimOutcomes map[string]string

	// AmbiguityDecidedNotAmbiguous records that the ambiguity question resolved to
	// a decided *negative*. It is tracked separately because material_ambiguity is
	// the one claim where a decided negative is the favourable answer, so
	// MaterialAmbiguity alone cannot distinguish "ruled not ambiguous" from "the
	// model could not tell" (PRD 17, 20).
	AmbiguityDecidedNotAmbiguous bool

	Model         string
	PolicyVersion string
}

// supported reports whether every bounded claim was decided in the extractor's
// favour. Anything undecided fails closed.
//
// The ambiguity claim is inverted relative to the others: a decided *negative*
// is what clears it. An answer in the undecided middle band means the bounded
// plane could not tell whether the email was ambiguous about the transaction, and
// that must hold the event for review rather than open the auto-confirm path
// (PRD 17). Reading it as "not ambiguous" is fail-open, which is what this
// previously did.
func (v EvidenceVerification) supported() bool {
	return v.TransactionObserved && v.AmountSupported && v.DirectionSupported && v.SemanticGrounded && v.AmbiguityDecidedNotAmbiguous
}

// jeverifier is the seam onto the bounded judgment plane. It is defined here, in
// the terms this package needs, so bank email never imports Telegram policy.
type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

// BankEmailVerificationPolicyVersion marks the thresholds that ruled on these
// verifications, so a stored decision stays reproducible (PRD §18).
const BankEmailVerificationPolicyVersion = "2026-09-jev3"

// evidenceVerificationPolicy is the bank-email slice of the shared threshold
// policy. A Noul here answers "does the email itself support this claim?".
var evidenceVerificationPolicy = struct {
	Amount    judgment.NoulPolicy
	Direction judgment.NoulPolicy
	Semantic  judgment.NoulPolicy
	Observed  judgment.NoulPolicy
	Ambiguity judgment.NoulPolicy
}{
	Amount:    judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Direction: judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Semantic:  judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Observed:  judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Ambiguity: judgment.NoulPolicy{High: 0.15, Low: 0.05},
}

// bankCategoryPolicy is the bounded-choice strictness for the new-merchant
// category question. It mirrors the Telegram category policy so a category is
// only auto-applied when the same plane would have accepted it there (PRD §9.3).
var bankCategoryPolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}

const bankCategoryQuestion = "Choose the best active expense category for this purchase. Use OTHER_OR_UNCLEAR only when no category is safe."

// resolveNewMerchantCategory asks the bounded plane to choose among the server's
// active categories for a new merchant, then returns the canonical category ID
// only when the answer is decisive and the chosen slug is one Go offered. It is
// the deterministic Go half of PRD §9.3: the model picks a slug, Go resolves the
// ID, and an undecided or ambiguous answer returns "" so the caller keeps its
// category-only review. A provider failure also returns "" rather than guessing.
func (p *Processor) resolveNewMerchantCategory(ctx context.Context, sourceEventID, householdID string, extraction Extraction) (string, categoryProvenance) {
	merchant, description, counterparty := strings.TrimSpace(value(extraction.Merchant)), strings.TrimSpace(value(extraction.Description)), strings.TrimSpace(value(extraction.Counterparty))
	if p.verifier == nil || (merchant == "" && description == "" && counterparty == "") {
		return "", categoryProvenance{}
	}
	categories, err := p.activeExpenseCategories(ctx, householdID)
	if err != nil || len(categories) == 0 {
		return "", categoryProvenance{}
	}
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
	}
	state := map[string]any{"amount_idr": value(extraction.AmountIDR)}
	if merchant != "" {
		state["merchant"] = "<untrusted_merchant>" + merchant + "</untrusted_merchant>"
	}
	if description != "" {
		state["description"] = "<untrusted_description>" + description + "</untrusted_description>"
	}
	if counterparty != "" {
		state["counterparty"] = "<untrusted_counterparty>" + counterparty + "</untrusted_counterparty>"
	}
	ctx = judgment.WithPhaseMetadata(ctx, "RESIDUAL_CATEGORY", BankEmailVerificationPolicyVersion)
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-category", judgment.Request{
		State: state,
		Questions: map[string]judgment.Question{
			"category": {Type: "choice", Instructions: bankCategoryQuestion, Criteria: judgment.CategoryCriteria(slugs)},
		},
	})
	if err != nil {
		return "", categoryProvenance{}
	}
	answer, ok := result.Answers["category"]
	if !ok || answer.Choice == "OTHER_OR_UNCLEAR" || !judgment.AcceptChoice(answer, judgment.CategoryCriteria(slugs), bankCategoryPolicy) {
		return "", categoryProvenance{}
	}
	for _, category := range categories {
		if category.Slug == answer.Choice {
			return category.ID, categoryProvenance{Model: result.Model, PolicyVersion: BankEmailVerificationPolicyVersion, Slug: answer.Choice, Accepted: true}
		}
	}
	return "", categoryProvenance{}
}

// categoryProvenance is the bounded answer that authorised a Jev-chosen
// category. It is persisted next to the mutation so an operator can tell a
// Jev-picked category from a deterministic merchant rule (ADR-038, PRD 15/16).
type categoryProvenance struct {
	Model         string
	PolicyVersion string
	Slug          string
	Accepted      bool
}

type expenseCategory struct{ ID, Slug string }

func (p *Processor) activeExpenseCategories(ctx context.Context, householdID string) ([]expenseCategory, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,slug FROM category WHERE household_id=$1 AND active AND parent_id IS NULL ORDER BY slug`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []expenseCategory
	for rows.Next() {
		var category expenseCategory
		if err := rows.Scan(&category.ID, &category.Slug); err != nil {
			return nil, err
		}
		out = append(out, category)
	}
	return out, rows.Err()
}

// verificationClaims is the bounded question set. Each claim is about facts the
// deterministic bank policy will act on, and each is answered from the same
// minimized state snapshot (PRD §20).
var verificationClaims = []struct {
	Key          string
	Instructions string
	Policy       judgment.NoulPolicy
}{
	{"transaction_observed", "Does the email report one real completed transaction (a purchase, payment, transfer, or fee the customer has made), as opposed to a promotion, statement, balance update, or unrelated notice? Answer yes when the email states a completed transaction, even if it also contains routine security or support boilerplate such as 'if you did not make this transaction, lock your card', 'contact us if this was not you', or a link to check your transaction history. Those protective footers do not make a completed transaction unreal or uncertain.", evidenceVerificationPolicy.Observed},
	{"amount_supported", "Is the extracted amount the amount this email states for its transaction? Answer yes when the email names that amount for the transaction; other numbers elsewhere in the email, such as a customer-service phone number, an OTP validity window, or a phone/SIM digit string, do not count as a competing transaction amount.", evidenceVerificationPolicy.Amount},
	{"direction_supported", "Does the email's wording support the extracted money direction (INCOMING or OUTGOING)? Answer yes when ordinary wording implies it, for example a debit-card or payment notification for OUTGOING and a transfer-received notice for INCOMING. Answer no only when the email suggests the opposite direction or none at all.", evidenceVerificationPolicy.Direction},
	{"semantic_grounded", "Does the email's wording support the canonical class Go will act on: an ordinary outgoing spend at a merchant, or movement of money to or from the customer's own accounts (transfer, internal transfer, RDN investment)? Answer yes when the wording clearly supports one of these. The exact payment mechanism (QR, debit card, merchant payment, ATM) does NOT matter here and must not lower the answer; answer no only when the wording suggests no real movement of money at all.", evidenceVerificationPolicy.Semantic},
	{"material_ambiguity", "Is this notification genuinely ambiguous about the transaction itself, for example two plausible transaction amounts, or two plausible transaction dates? Routine email boilerplate is not ambiguity: a security footer like 'if this was not you, lock your card', a support phone number, or a link to view your history does not make the transaction ambiguous.", evidenceVerificationPolicy.Ambiguity},
}

// verifyEvidence asks one bounded bundle about an already-extracted bank email.
// The provider never produces merchant or date strings here; it only rules on
// claims Go already holds. A provider failure is returned as an error so the
// caller can take the safe retry/review path instead of trusting confidence.
//
// A provider failure is the one case PRD §3.7 authorizes a safe retry for
// (timeout, gateway error, rate limit, malformed response), so a failed call is
// re-asked once. A decisive negative ruling is a verdict, not a failure;
// re-asking would be an OR over two draws that raises acceptance above policy.
// Negative rulings therefore park the email for review immediately.
func (p *Processor) verifyEvidence(ctx context.Context, sourceEventID string, extraction Extraction, email TrustedEmail) (EvidenceVerification, bool, error) {
	if p.verifier == nil {
		return EvidenceVerification{}, false, nil
	}
	verification, verified, err := p.verifyEvidenceOnce(ctx, sourceEventID, extraction, email)
	if err == nil {
		return verification, verified, err
	}
	retry, retried, retryErr := p.verifyEvidenceOnce(ctx, sourceEventID+"-reask", extraction, email)
	if retryErr != nil {
		return EvidenceVerification{}, false, retryErr
	}
	return retry, retried, nil
}

func (p *Processor) verifyEvidenceOnce(ctx context.Context, requestID string, extraction Extraction, email TrustedEmail) (EvidenceVerification, bool, error) {
	questions := make(map[string]judgment.Question, len(verificationClaims))
	for _, claim := range verificationClaims {
		questions[claim.Key] = judgment.Question{Type: "noul", Instructions: claim.Instructions}
	}
	ctx = judgment.WithPhaseMetadata(ctx, "EVIDENCE_SUPPORT", BankEmailVerificationPolicyVersion)
	result, err := p.verifier.Evaluate(ctx, requestID+"-verify", judgment.Request{
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
	verification.SemanticGrounded = noulClaimed(result.Answers, "semantic_grounded", evidenceVerificationPolicy.Semantic)
	verification.MaterialAmbiguity, verification.AmbiguityDecidedNotAmbiguous = ambiguityVerdict(result.Answers, "material_ambiguity", evidenceVerificationPolicy.Ambiguity)
	verification.ClaimOutcomes = make(map[string]string, len(verificationClaims))
	policies := map[string]judgment.NoulPolicy{
		"transaction_observed": evidenceVerificationPolicy.Observed,
		"amount_supported":     evidenceVerificationPolicy.Amount,
		"direction_supported":  evidenceVerificationPolicy.Direction,
		"semantic_grounded":    evidenceVerificationPolicy.Semantic,
		"material_ambiguity":   evidenceVerificationPolicy.Ambiguity,
	}
	for _, claim := range verificationClaims {
		status := "UNDECIDED"
		if answer, ok := result.Answers[claim.Key]; ok {
			yes, decided := judgment.AcceptNoul(answer, policies[claim.Key])
			if decided {
				status = "NO"
				if yes {
					status = "YES"
				}
			}
		}
		verification.ClaimOutcomes[claim.Key] = status
	}
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

// ambiguityVerdict reads the inverted claim. It returns (isAmbiguous,
// decidedNotAmbiguous) so a caller can require an affirmative not-ambiguous
// ruling instead of treating an undecided answer as approval.
func ambiguityVerdict(answers map[string]judgment.Answer, key string, policy judgment.NoulPolicy) (bool, bool) {
	answer, ok := answers[key]
	if !ok {
		return false, false
	}
	ambiguous, decided := judgment.AcceptNoul(answer, policy)
	return ambiguous, decided && !ambiguous
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
