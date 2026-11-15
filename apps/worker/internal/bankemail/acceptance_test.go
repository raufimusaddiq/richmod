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

// Bank Email acceptance tests, named by acceptance case; offline except B2, which needs TEST_DATABASE_URL.
// Bounded verdicts are stubbed; the real provider is covered by canary_corpus_test.go.

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

// B1 also pins zero human touch: the confirm path must not be a review wearing a different status.
func TestBankEmailB1LearnedMerchantCreatesNoReviewWork(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.ReviewType != "" {
		t.Fatalf("a confirmed expense must carry no review type: %+v", result)
	}
}

// B2 - a new merchant whose category the bounded plane decided confirms with it and creates no review work.
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

	// The policy still starts an unknown merchant as a category review (pinned by B3).
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("an unknown merchant starts as a category review: %+v", result)
	}

	verifier := &stubVerifier{answers: map[string]judgment.Answer{
		"category": choice("makanan-minuman", judgment.CategoryCriteria([]judgment.CategoryChoice{{Slug: "makanan-minuman"}})),
	}}
	processor := &Processor{pool: pool, verifier: verifier}
	// The decisive answer must turn the category review into a confirmation with no review left behind.
	decided, err := processor.applyCategoryDecision(ctx, "se-1", householdID, outgoingCard("54000", "Warung Baru"), result)
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != "CONFIRMED" || !decided.AutoConfirm || decided.ReviewType != "" || decided.CategoryID != foodID {
		t.Fatalf("a decisive category must confirm with no review: %+v", decided)
	}

	// A provider failure is a machine failure and must surface as a retryable error, so no category review
	// reaches the household.
	undecided := &Processor{pool: pool, verifier: &stubVerifier{err: errors.New("provider down")}}
	kept, err := undecided.applyCategoryDecision(ctx, "se-1", householdID, outgoingCard("54000", "Warung Baru"), result)
	if err == nil {
		t.Fatalf("a provider failure must propagate as a retryable machine error, got kept=%+v", kept)
	}
}

// B3 - an undecided new merchant stays a category-only review; the known amount and time are never re-requested.
func TestBankEmailB3UndecidedCategoryAsksOnlyForCategory(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("undecided category must park a category review: %+v", result)
	}
	if result.AutoConfirm {
		t.Fatalf("an undecided category must never auto-confirm: %+v", result)
	}
	// The undecided path is a category review whose rendered message states amount and time as context;
	// asserting the message, not a hand-built decision, proves they are not re-requested.
	message := bankReviewMessage(result.ReviewType, "54000", time.Date(2026, 9, 23, 13, 45, 0, 0, time.UTC), "Warung Baru")
	if !strings.Contains(message, "Rp54.000") || !strings.Contains(message, "WIB") {
		t.Fatalf("the review must already show the known amount and time: %q", message)
	}
	if strings.Contains(strings.ToLower(message), "nominal baru") || strings.Contains(strings.ToLower(message), "isi waktu") {
		t.Fatalf("the review must not ask for amount or time again: %q", message)
	}
}

// B4 - merchant absent but every other fact valid: still classified; a missing merchant is a missing fact,
// not an invalid transaction.
func TestBankEmailB4MissingMerchantDoesNotBecomeUnknownPurpose(t *testing.T) {
	channel, direction := "QR", "OUTGOING"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction}
	result := EvaluateBankEmail(spendingListener(), extraction, nil)
	if result.Type != "EXPENSE" || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("valid facts with no merchant must stay an expense: %+v", result)
	}
	if result.ReviewType != "UNKNOWN_MERCHANT" {
		t.Fatalf("missing merchant must keep its merchant-first review contract: %+v", result)
	}
}

// UNKNOWN_MERCHANT collects the name before category inference on every channel.
func TestBankEmailMerchantlessExpenseCollectsMerchantBeforeCategory(t *testing.T) {
	for _, channel := range []string{"DEBIT_CARD", "MERCHANT_PAYMENT", "QR", "ATM", "BANK_FEE", "OTHER"} {
		t.Run(channel, func(t *testing.T) {
			direction, description := "OUTGOING", "Pembayaran makan siang di kantin"
			extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Description: &description}
			result := EvaluateBankEmail(spendingListener(), extraction, nil)
			verifier := &stubVerifier{err: errors.New("must not call category inference")}
			kept, err := (&Processor{verifier: verifier}).applyCategoryDecision(context.Background(), "source", "household", extraction, result)
			if err != nil || kept.Status != "NEEDS_REVIEW" || kept.ReviewType != "UNKNOWN_MERCHANT" || kept.AutoConfirm || verifier.calls != 0 {
				t.Fatalf("merchant must precede category: result=%+v calls=%d err=%v", kept, verifier.calls, err)
			}
			decision := transactionReviewDecision("household", "source", extraction, kept, "transaction")
			if len(decision.MissingFacts) != 2 || decision.MissingFacts[0] != "merchant" || decision.MissingFacts[1] != "category" {
				t.Fatalf("merchant-first contract=%v", decision.MissingFacts)
			}
		})
	}
}

// B4, second half: the merchant is nullable in the tool contract so the model never invents one; every
// property is still `required` since a native tool call must be complete.
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

// B6 - provider failure is infrastructure, never a semantic verdict: it surfaces as an error, and an
// unconfigured verifier never reads as approval.
func TestBankEmailB6ProviderFailureIsNotApproval(t *testing.T) {
	failing := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, verified, err := failing.verifyEvidence(context.Background(), "source", Extraction{}, TrustedEmail{}); err == nil || verified {
		t.Fatalf("provider failure must surface as an error, verified=%v err=%v", verified, err)
	}

	unconfigured := NewProcessor(nil, nil)
	// Verification is mandatory: a missing plane is machine retry state, never a review or approval.
	if _, verified, err := unconfigured.verifyEvidence(nil, "source", Extraction{}, TrustedEmail{}); err == nil || verified {
		t.Fatalf("a nil verifier must be a machine failure, verified=%v err=%v", verified, err)
	}
}

func spendingListener() Listener { return Listener{TrackingPolicy: "SPENDING_ONLY", Active: true} }

func outgoingCard(amount, merchant string) Extraction {
	channel, direction := "DEBIT_CARD", "OUTGOING"
	return Extraction{Kind: "TRANSACTION", AmountIDR: ptr(amount), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Merchant: ptr(merchant)}
}
