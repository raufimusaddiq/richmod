package bankemail

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/categoryctx"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// errVerifierUnconfigured marks a missing bounded plane; verification is mandatory for the bank source, so it
// is a machine retry, never a household question.
var errVerifierUnconfigured = errors.New("bank email evidence verifier is not configured")

// EvidenceVerification is the bounded ruling over one extracted bank notification: whether its facts are safe
// to hand to the deterministic bank policy.
type EvidenceVerification struct {
	TransactionObserved bool
	AmountSupported     bool
	DirectionSupported  bool
	// SemanticGrounded: the email's wording supports the class the policy acts on (spend vs transfer).
	// The payment mechanism is only metadata, so an uncertain one must not block an otherwise safe expense.
	SemanticGrounded bool

	// ClaimOutcomes keeps YES/NO/UNDECIDED per predicate so a review names the exact material predicate that
	// did not clear.
	ClaimOutcomes map[string]string

	Model         string
	PolicyVersion string
}

// supported reports whether every bounded claim was decided in the extractor's favour; undecided fails.
// Only concrete support is checked, not negatives like "not ambiguous", which would force needless review.
func (v EvidenceVerification) supported() bool {
	return v.TransactionObserved && v.AmountSupported && v.DirectionSupported && v.SemanticGrounded
}

// jeverifier is the seam onto the judgment plane, in this package's terms so bank email never imports Telegram policy.
type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

// BankEmailVerificationPolicyVersion marks the thresholds that ruled on these verifications so stored
// decisions stay reproducible.
const BankEmailVerificationPolicyVersion = "2026-09-jev4"

// BankEmailCategoryPolicyVersion marks the category question's context and thresholds (ADR-053).
const BankEmailCategoryPolicyVersion = "2026-10-bank-category2"

// evidenceVerificationPolicy is the bank-email slice of the shared threshold policy; a Noul answers whether
// the email supports the claim.
var evidenceVerificationPolicy = struct {
	Amount    judgment.NoulPolicy
	Direction judgment.NoulPolicy
	Semantic  judgment.NoulPolicy
	Observed  judgment.NoulPolicy
}{
	Amount:    judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Direction: judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Semantic:  judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Observed:  judgment.NoulPolicy{High: 0.85, Low: 0.15},
}

// bankCategoryPolicy mirrors the Telegram category policy so a category auto-applies only when the same plane
// would accept it there.
var bankCategoryPolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}

const bankCategoryQuestion = "Choose the best active expense category for this purchase. Use OTHER_OR_UNCLEAR only when no category is safe."

// resolveNewMerchantCategory asks the bounded plane to pick an active category for a new merchant, returning
// an ID only for a decisive, offered slug. A provider or DB error is returned so the job retries.
func (p *Processor) resolveNewMerchantCategory(ctx context.Context, sourceEventID, householdID string, extraction Extraction) (string, categoryProvenance, error) {
	merchant, description, counterparty := strings.TrimSpace(value(extraction.Merchant)), strings.TrimSpace(value(extraction.Description)), strings.TrimSpace(value(extraction.Counterparty))
	if merchant == "" && description == "" && counterparty == "" {
		// No usable evidence can support a category question; the category residual stands without a machine decision.
		return "", categoryProvenance{}, nil
	}
	if p.verifier == nil {
		return "", categoryProvenance{}, errVerifierUnconfigured
	}
	categories, err := p.activeExpenseCategories(ctx, householdID)
	if err != nil {
		return "", categoryProvenance{}, err
	}
	if len(categories) == 0 {
		// This household asserts no active expense categories, so there is no bounded choice; not a machine failure.
		return "", categoryProvenance{}, nil
	}
	slugs := make([]string, 0, len(categories))
	choices := make([]judgment.CategoryChoice, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
		choices = append(choices, judgment.CategoryChoice{Slug: category.Slug, Label: category.Name})
	}
	criteria := judgment.CategoryCriteria(choices)
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
	// Same-merchant history and purchase time are facts the model needs to rule on a merchant it cannot tell
	// from its name alone (ADR-053).
	history, err := categoryctx.History(ctx, p.pool, householdID, merchant, slugs)
	if err != nil {
		return "", categoryProvenance{}, err
	}
	if history != nil {
		state[categoryctx.StateKey] = history
	}
	if extraction.TransactionAt != nil {
		state["local_time"] = categoryctx.LocalTime(*extraction.TransactionAt)
	}
	// Category is the single open dimension; this phase does not re-decide an accepted category.
	ctx = judgment.WithPhaseMetadata(ctx, "RESIDUAL_CATEGORY", BankEmailCategoryPolicyVersion, []string{})
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-category", judgment.Request{
		State: state,
		Questions: map[string]judgment.Question{
			"category": {Type: "choice", Instructions: bankCategoryQuestion, Criteria: criteria},
		},
	})
	if err != nil {
		return "", categoryProvenance{}, err
	}
	answer, ok := result.Answers["category"]
	if !ok || answer.Choice == "OTHER_OR_UNCLEAR" || !judgment.AcceptChoice(answer, criteria, bankCategoryPolicy) {
		return "", categoryProvenance{}, nil
	}
	for _, category := range categories {
		if category.Slug == answer.Choice {
			return category.ID, categoryProvenance{Model: result.Model, PolicyVersion: BankEmailCategoryPolicyVersion, Slug: answer.Choice, Accepted: true}, nil
		}
	}
	return "", categoryProvenance{}, nil
}

// categoryProvenance is the bounded answer that authorised a Jev-chosen category, persisted with the mutation
// to tell it from a deterministic merchant rule (ADR-038).
type categoryProvenance struct {
	Model         string
	PolicyVersion string
	Slug          string
	Accepted      bool
}

type expenseCategory struct{ ID, Slug, Name string }

func (p *Processor) activeExpenseCategories(ctx context.Context, householdID string) ([]expenseCategory, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,slug,name FROM category WHERE household_id=$1 AND active AND parent_id IS NULL ORDER BY slug`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []expenseCategory
	for rows.Next() {
		var category expenseCategory
		if err := rows.Scan(&category.ID, &category.Slug, &category.Name); err != nil {
			return nil, err
		}
		out = append(out, category)
	}
	return out, rows.Err()
}

// verificationClaims is the bounded question set about facts the deterministic bank policy acts on, all
// answered from one minimized state snapshot.
var verificationClaims = []struct {
	Key          string
	Instructions string
	Policy       judgment.NoulPolicy
}{
	{"transaction_observed", "Does the email report one real completed transaction (a purchase, payment, transfer, or fee the customer has made), as opposed to a promotion, statement, balance update, or unrelated notice? Answer yes when the email states a completed transaction, even if it also contains routine security or support boilerplate such as 'if you did not make this transaction, lock your card', 'contact us if this was not you', or a link to check your transaction history. Those protective footers do not make a completed transaction unreal or uncertain.", evidenceVerificationPolicy.Observed},
	{"amount_supported", "Is the extracted amount the amount this email states for its transaction? Answer yes when the email names that amount for the transaction; other numbers elsewhere in the email, such as a customer-service phone number, an OTP validity window, or a phone/SIM digit string, do not count as a competing transaction amount.", evidenceVerificationPolicy.Amount},
	{"direction_supported", "Does the email's wording support the extracted money direction (INCOMING or OUTGOING)? Answer yes when ordinary wording implies it, for example a debit-card or payment notification for OUTGOING and a transfer-received notice for INCOMING. Answer no only when the email suggests the opposite direction or none at all.", evidenceVerificationPolicy.Direction},
	{"semantic_grounded", "Does the email's wording support the canonical class Go will act on: an ordinary outgoing spend at a merchant, or movement of money to or from the customer's own accounts (transfer, internal transfer, RDN investment)? Answer yes when the wording clearly supports one of these. The exact payment mechanism (QR, debit card, merchant payment, ATM) does NOT matter here and must not lower the answer; answer no only when the wording suggests no real movement of money at all.", evidenceVerificationPolicy.Semantic},
}

// verifyEvidence asks one bounded bundle about an extracted email; a provider failure is re-asked once.
// A decisive negative is a verdict, not a failure: re-asking would raise acceptance above policy.
func (p *Processor) verifyEvidence(ctx context.Context, sourceEventID string, extraction Extraction, email TrustedEmail) (EvidenceVerification, bool, error) {
	if p.verifier == nil {
		// Verification is mandatory for this source; an unconfigured plane is infrastructure state to retry,
		// never a household review.
		return EvidenceVerification{}, false, errVerifierUnconfigured
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
		// No ruling was obtained: infrastructure failure, never a verified or implicitly approved verdict.
		return EvidenceVerification{}, false, err
	}
	verification := EvidenceVerification{Model: result.Model, PolicyVersion: BankEmailVerificationPolicyVersion}
	verification.TransactionObserved = noulClaimed(result.Answers, "transaction_observed", evidenceVerificationPolicy.Observed)
	verification.AmountSupported = noulClaimed(result.Answers, "amount_supported", evidenceVerificationPolicy.Amount)
	verification.DirectionSupported = noulClaimed(result.Answers, "direction_supported", evidenceVerificationPolicy.Direction)
	verification.SemanticGrounded = noulClaimed(result.Answers, "semantic_grounded", evidenceVerificationPolicy.Semantic)
	verification.ClaimOutcomes = make(map[string]string, len(verificationClaims))
	policies := map[string]judgment.NoulPolicy{
		"transaction_observed": evidenceVerificationPolicy.Observed,
		"amount_supported":     evidenceVerificationPolicy.Amount,
		"direction_supported":  evidenceVerificationPolicy.Direction,
		"semantic_grounded":    evidenceVerificationPolicy.Semantic,
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

// noulClaimed reports a decided affirmative Noul; the middle band fails closed like the Telegram decision plane.
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
