package bankemail

import (
	"strings"
	"testing"
	"time"
)

func TestBankEmailB1LearnedMerchantConfirmsWithoutReview(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.Status != "CONFIRMED" || !result.AutoConfirm {
		t.Fatalf("learned merchant must confirm: %+v", result)
	}
	if result.CategoryID != "cat-food" {
		t.Fatalf("learned category must be reused: %+v", result)
	}
}

func TestBankEmailB1LearnedMerchantCreatesNoReviewWork(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Toko Sumber Rejeki"), nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	if result.ReviewType != "" {
		t.Fatalf("a confirmed expense must carry no review type: %+v", result)
	}
}

func TestBankEmailB2ExtractedCategoryConfirmsWithoutReview(t *testing.T) {
	extraction := outgoingCard("54000", "Warung Baru")
	slug := "makanan-minuman"
	extraction.CategorySlug = &slug
	extraction.Confidence = ExtractedCategoryMinConfidence
	categories := []CategoryOption{{ID: "cat-food", Slug: "makanan-minuman", Name: "Makanan & Minuman"}}

	result := EvaluateBankEmail(spendingListener(), extraction, nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("an unknown merchant starts as a category review: %+v", result)
	}

	decided := applyExtractedCategory(extraction, categories, result)
	if decided.Status != "CONFIRMED" || !decided.AutoConfirm || decided.ReviewType != "" || decided.CategoryID != "cat-food" {
		t.Fatalf("an extracted active household category at threshold must confirm with no review: %+v", decided)
	}

	lowConfidence := extraction
	lowConfidence.Confidence = ExtractedCategoryMinConfidence - 0.01
	held := applyExtractedCategory(lowConfidence, categories, result)
	if held.Status != "NEEDS_REVIEW" || held.AutoConfirm || held.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("low confidence extracted category must stay in review: %+v", held)
	}

	colliding := []CategoryOption{
		{ID: "cat-root", Slug: "makanan-minuman", Name: "Makanan & Minuman"},
		{ID: "cat-child", Slug: "makanan-minuman", Name: "Harian > Makanan & Minuman"},
		{ID: "cat-unique", Slug: "transportasi", Name: "Transportasi"},
	}
	ambiguous := applyExtractedCategory(extraction, colliding, result)
	if ambiguous.Status != "NEEDS_REVIEW" || ambiguous.AutoConfirm || ambiguous.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("duplicate category slug must never auto-confirm: %+v", ambiguous)
	}
	tool := EmitBankTransactionTool(colliding...)
	props := tool.Parameters["properties"].(map[string]any)
	catEnum := props["category_slug"].(map[string]any)["enum"].([]any)
	if len(catEnum) != 2 || catEnum[0] != "transportasi" || catEnum[1] != nil {
		t.Fatalf("duplicate slug must be excluded from schema enum: %v", catEnum)
	}

	unknownSlug := "not-a-household-category"
	extraction.CategorySlug = &unknownSlug
	kept := applyExtractedCategory(extraction, categories, result)
	if kept.Status != "NEEDS_REVIEW" || kept.AutoConfirm || kept.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("an unknown extracted category slug must stay in review: %+v", kept)
	}
}

func TestBankEmailMerchantMemoryWinsOverExtractedCategory(t *testing.T) {
	extraction := outgoingCard("54000", "Toko Sumber Rejeki")
	slug := "lainnya"
	extraction.CategorySlug = &slug
	categories := []CategoryOption{
		{ID: "cat-food", Slug: "makanan-minuman", Name: "Makanan & Minuman"},
		{ID: "cat-other", Slug: "lainnya", Name: "Lainnya"},
	}

	result := EvaluateBankEmail(spendingListener(), extraction, nil, MerchantMemory{MerchantID: "m-1", CategoryID: "cat-food", AutoApply: true})
	final := applyExtractedCategory(extraction, categories, result)
	if final.CategoryID != "cat-food" || !final.AutoConfirm {
		t.Fatalf("merchant memory must win over extracted category: %+v", final)
	}
}

func TestBankEmailB3UndecidedCategoryAsksOnlyForCategory(t *testing.T) {
	result := EvaluateBankEmail(spendingListener(), outgoingCard("54000", "Warung Baru"), nil)
	if result.Status != "NEEDS_REVIEW" || result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("undecided category must park a category review: %+v", result)
	}
	if result.AutoConfirm {
		t.Fatalf("an undecided category must never auto-confirm: %+v", result)
	}
	message := bankReviewMessage(result.ReviewType, "54000", time.Date(2026, 9, 23, 13, 45, 0, 0, time.UTC), "Warung Baru")
	if !strings.Contains(message, "Rp54.000") || !strings.Contains(message, "WIB") {
		t.Fatalf("the review must already show the known amount and time: %q", message)
	}
	if strings.Contains(strings.ToLower(message), "nominal baru") || strings.Contains(strings.ToLower(message), "isi waktu") {
		t.Fatalf("the review must not ask for amount or time again: %q", message)
	}
}

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

func TestBankEmailMerchantlessExpenseCollectsMerchantBeforeCategory(t *testing.T) {
	categories := []CategoryOption{{ID: "cat-food", Slug: "makanan-minuman", Name: "Makanan & Minuman"}}
	slug := "makanan-minuman"
	for _, channel := range []string{"DEBIT_CARD", "MERCHANT_PAYMENT", "QR", "ATM", "BANK_FEE", "OTHER"} {
		t.Run(channel, func(t *testing.T) {
			direction, description := "OUTGOING", "Pembayaran makan siang di kantin"
			extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("25000"), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Description: &description, CategorySlug: &slug, Confidence: ExtractedCategoryMinConfidence}
			result := EvaluateBankEmail(spendingListener(), extraction, nil)
			kept := applyExtractedCategory(extraction, categories, result)
			if kept.Status != "NEEDS_REVIEW" || kept.ReviewType != "UNKNOWN_MERCHANT" || kept.AutoConfirm {
				t.Fatalf("merchant must precede category: result=%+v", kept)
			}
			decision := transactionReviewDecision("household", "source", extraction, kept, false, "transaction")
			if len(decision.MissingFacts) != 2 || decision.MissingFacts[0] != "merchant" || decision.MissingFacts[1] != "category" {
				t.Fatalf("merchant-first contract=%v", decision.MissingFacts)
			}
		})
	}
}

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

func spendingListener() Listener { return Listener{TrackingPolicy: "SPENDING_ONLY", Active: true} }

func outgoingCard(amount, merchant string) Extraction {
	channel, direction := "DEBIT_CARD", "OUTGOING"
	return Extraction{Kind: "TRANSACTION", AmountIDR: ptr(amount), TransactionAt: timePtr(), Channel: &channel, Direction: &direction, Merchant: ptr(merchant)}
}
