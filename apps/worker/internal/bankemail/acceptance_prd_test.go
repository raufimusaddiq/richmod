package bankemail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// PRD section 25 required Bank Email acceptance tests, named by their PRD case so
// a reviewer can map each one to the requirement.
//
// Every case here is offline and deterministic except B2, which touches the real
// category table through TEST_DATABASE_URL because the PRD (9.3) makes the
// canonical ID resolution a Go responsibility. The bounded *verdict* is stubbed:
// asserting what a decisive or undecided answer must land on is the deterministic
// half this file owns. The answers themselves are exercised against the real
// provider by the section 23 semantic canary corpus in canary_corpus_test.go (PR #132),
// which runs the same corpus against the live gateway when it is configured.

// B1 - learn merchant auto-applies its stored category.
func TestBankEmailB1LearnedMerchantConfirmsWithoutReview(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.Status != "CONFIRMED" || !result.AutoConfirm {
		t.Fatalf("learned merchant must confirm: %+v", result)
	}
	if result.CategoryID != "cat-food" {
		t.Fatalf("learned category must be reused: %+v", result)
	}
}

// B1 also pins the zero-human-touch property the PRD states as RHICE = 0: the
// confirm path must not be a review wearing a different status.
func TestBankEmailB1LearnedMerchantCreatesNoReviewWork(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.ReviewType != "" {
		t.Fatalf("a confirmed expense must carry no review type: %+v", result)
	}
}

// B2 - a new merchant whose category the bounded plane decided must confirm with
// that category and create no review work. This is the deterministic half of the
// case: the processor writes these fields onto the policy result when the
// classifier returns a decisive answer.
func TestBankEmailB2DecisiveCategoryConfirmsWithoutReview(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var householdID string
	if err := pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("B2 %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	var foodID string
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman') RETURNING id", householdID).Scan(&foodID); err != nil {
		t.Fatal(err)
	}

	// The policy itself still starts the unknown merchant as a category review;
	// that is unchanged and is what B3 pins.
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("an unknown merchant starts as a category review: %+v", result)
	}

	// A decisive bounded answer resolves the canonical slug to its ID.
	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"category": choice("makanan-minuman", judgment.CategoryCriteria([]string{"makanan-minuman"})),
	}}
	processor := &Processor{pool: pool, verifier: verifier}
	// The decisive answer must turn the category review into a confirmation with no
	// review left behind, not merely resolve an id.
	decided := processor.applyCategoryDecision(ctx, "se-1", householdID, outgoingCard("54000", "Warung Baru"), result)
	if decided.Status != "CONFIRMED" || !decided.AutoConfirm || decided.ReviewType != "" || decided.CategoryID != foodID {
		t.Fatalf("a decisive category must confirm with no review: %+v", decided)
	}

	// A provider failure must leave the review in place rather than guess.
	undecided := &Processor{pool: pool, verifier: &stubVerifier{err: errors.New("provider down")}}
	kept := undecided.applyCategoryDecision(ctx, "se-1", householdID, outgoingCard("54000", "Warung Baru"), result)
	if kept.ReviewType != "AMBIGUOUS_CATEGORY" || kept.Status != "NEEDS_REVIEW" || kept.AutoConfirm {
		t.Fatalf("a provider failure must keep the category review: %+v", kept)
	}
}

// B3 - a new merchant whose category the bounded plane could not decide stays a
// review, and that review is category-only: the amount and the time are already
// known and must never be re-requested (PRD 3.3, 18.1).
func TestBankEmailB3UndecidedCategoryAsksOnlyForCategory(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("undecided category must park a category review: %+v", result)
	}
	if result.AutoConfirm {
		t.Fatalf("an undecided category must never auto-confirm: %+v", result)
	}
	// The undecided path is a transaction-backed category review: the message the
	// user sees states the amount and time as context and asks only for a category
	// (PRD 9.6). Asserting the rendered message is what proves the amount and date
	// are not re-requested; asserting a hand-built decision's missingFacts would
	// only echo the input.
	message := bankReviewMessage(result.ReviewType, "54000", time.Date(2026, 9, 23, 13, 45, 0, 0, time.UTC), "Warung Baru")
	if !strings.Contains(message, "Rp54.000") || !strings.Contains(message, "WIB") {
		t.Fatalf("the review must already show the known amount and time: %q", message)
	}
	if strings.Contains(strings.ToLower(message), "nominal baru") || strings.Contains(strings.ToLower(message), "isi waktu") {
		t.Fatalf("the review must not ask for amount or time again: %q", message)
	}
}

// B4 - merchant absent but every other fact valid. The expense must still be
// classified; a missing merchant is a missing fact, not an invalid transaction.
func TestBankEmailB4MissingMerchantDoesNotBecomeUnknownPurpose(t *testing.T) {
	channel, direction := "QR", "OUTGOING"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction}
	result := EvaluateBankEmail(spendingListener(), extraction, nil)
	if result.Type != "EXPENSE" || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("valid facts with no merchant must stay an expense: %+v", result)
	}
	if result.ReviewType != "UNKNOWN_MERCHANT" {
		t.Fatalf("missing merchant must keep its review contract until category rescue: %+v", result)
	}
}

// IR-07: absence of a merchant is not itself a canonical requirement, so the
// bounded plane still resolves the category from the other supported evidence,
// and the merchant stays NULL (no fabrication).
func TestBankEmailMerchantlessCategoryRescueConfirmsWithoutMerchant(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var householdID, categoryID string
	if err := pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("IR07 %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman') RETURNING id", householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}

	channel, direction := "QR", "OUTGOING"
	description := "Pembayaran makan siang di kantin"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Description: &description}
	result := EvaluateBankEmail(spendingListener(), extraction, nil)
	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"category": choice("makanan-minuman", judgment.CategoryCriteria([]string{"makanan-minuman"})),
	}}
	processor := &Processor{pool: pool, verifier: verifier}
	decided := processor.applyCategoryDecision(ctx, "se-1", householdID, extraction, result)
	if decided.Status != "CONFIRMED" || !decided.AutoConfirm || decided.ReviewType != "" || decided.CategoryID != categoryID {
		t.Fatalf("a decisive merchant-less category must confirm: %+v", decided)
	}
	if extraction.Merchant != nil {
		t.Fatalf("the rescue must not fabricate a merchant: %q", *extraction.Merchant)
	}
	state, ok := verifier.request.State.(map[string]any)
	if !ok || verifier.calls != 1 || state["merchant"] != nil || state["description"] == nil {
		t.Fatalf("merchant-less rescue must use only supplied evidence: calls=%d state=%v", verifier.calls, verifier.request.State)
	}

	undecided := &Processor{pool: pool, verifier: &stubVerifier{err: errors.New("provider down")}}
	kept := undecided.applyCategoryDecision(ctx, "se-1", householdID, extraction, result)
	if kept.Status != "NEEDS_REVIEW" || kept.ReviewType != "UNKNOWN_MERCHANT" || kept.AutoConfirm {
		t.Fatalf("a provider failure must keep the existing merchant/category review: %+v", kept)
	}
}

// B4, second half: the merchant value is nullable in the tool contract, so the
// model is never forced to invent one (PRD 9.5). Every property is listed in
// `required` because a native tool call must be structurally complete; it is the
// nullable *type* that keeps the value un-fabricated.
func TestBankEmailB4MerchantValueIsNullable(t *testing.T) {
	tool := EmitBankTransactionTool()
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("tool schema has no properties map")
	}
	merchant, ok := properties["merchant"].(map[string]any)
	if !ok {
		t.Fatal("tool schema does not describe merchant")
	}
	types, ok := merchant["type"].([]string)
	if !ok {
		t.Fatalf("merchant type should be a nullable union, got %T", merchant["type"])
	}
	nullable := false
	for _, candidate := range types {
		if candidate == "null" {
			nullable = true
		}
	}
	if !nullable {
		t.Fatalf("merchant must be nullable so it is never fabricated: %v", types)
	}
}

// B5 - two plausible transaction amounts. This asserts the property the case is
// named for: an undecided ambiguity ruling must not authorise a write, while a
// decided not-ambiguous ruling must. The ambiguity claim is inverted, so the two
// bands have to be separated by the verdict helper rather than by AcceptNoul
// alone; asserting only `if noulClaimed(...)` would pass either way and prove
// nothing (Hermes review).
func TestBankEmailB5UndecidedAmbiguityDoesNotAuthorize(t *testing.T) {
	// 0.10 sits between Low 0.05 and High 0.15: the plane could not tell whether
	// the email was ambiguous, which is exactly the two-amount case.
	answers := supportedRuling()
	answers["material_ambiguity"] = noul(0.10)
	verification, verified, err := (&Processor{verifier: &stubVerifier{answers: answers}}).verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
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
		t.Fatalf("an undecided answer must not read as decided-not-ambiguous: %+v", verification)
	}
	if verification.supported() {
		t.Fatalf("an undecided ambiguity ruling must not authorize: %+v", verification)
	}
}

// B5, other half: only an affirmative not-ambiguous ruling clears the gate.
func TestBankEmailB5DecidedNotAmbiguousAuthorizes(t *testing.T) {
	verification, verified, err := (&Processor{verifier: &stubVerifier{answers: supportedRuling()}}).verifyEvidence(context.Background(), "src", testExtraction(), TrustedEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if !verified || !verification.AmbiguityDecidedNotAmbiguous || !verification.supported() {
		t.Fatalf("a decided not-ambiguous ruling must authorize: %+v", verification)
	}
}

// B6 - provider failure is an infrastructure event, never a semantic verdict.
// The caller must be able to tell "no ruling" from "ruled safe": a failure is
// surfaced as an error, and an unconfigured verifier is the disabled case that
// still never reads as approval.
func TestBankEmailB6ProviderFailureIsNotApproval(t *testing.T) {
	failing := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, verified, err := failing.verifyEvidence(context.Background(), "source", Extraction{}, TrustedEmail{}); err == nil || verified {
		t.Fatalf("provider failure must surface as an error, verified=%v err=%v", verified, err)
	}

	unconfigured := NewProcessor(nil, nil)
	verification, verified, err := unconfigured.verifyEvidence(nil, "source", Extraction{}, TrustedEmail{})
	if err != nil {
		t.Fatalf("a nil verifier is the disabled case, not a failure: %v", err)
	}
	if verified {
		t.Fatal("a nil verifier must not report a verified email")
	}
	if verification != (EvidenceVerification{}) {
		t.Fatalf("a nil verifier must return the zero verification: %+v", verification)
	}
}

func spendingListener() Listener { return Listener{TrackingPolicy: "SPENDING_ONLY", Active: true} }

func outgoingCard(amount, merchant string) Extraction {
	channel, direction := "DEBIT_CARD", "OUTGOING"
	return Extraction{Kind: "TRANSACTION", AmountIDR: ptr(amount), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Merchant: ptr(merchant)}
}
