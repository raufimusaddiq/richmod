package bankemail

import (
	"testing"
	"time"
)

func TestEvaluateBankEmailPolicy(t *testing.T) {
	listener := Listener{TrackingPolicy: "SPENDING_ONLY"}
	incoming := "INCOMING"
	result := EvaluateBankEmail(listener, Extraction{Kind: "TRANSACTION", Direction: &incoming, AmountIDR: ptr("1"), TransactionAt: timePtr()}, nil)
	if result.Type != "IGNORE" {
		t.Fatalf("incoming=%+v", result)
	}
	outgoing := "OUTGOING"
	channel := "QR"
	result = EvaluateBankEmail(listener, Extraction{Kind: "TRANSACTION", Direction: &outgoing, Channel: &channel, AmountIDR: ptr("1"), TransactionAt: timePtr()}, nil)
	if result.Type != "EXPENSE" || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("expense=%+v", result)
	}
}

func TestApplyEmailReceivedTimeFallback(t *testing.T) {
	receivedAt := time.Date(2026, 8, 31, 3, 32, 46, 0, time.FixedZone("UTC+7", 7*60*60))
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("13120"), MissingFields: []string{"transaction_at", "merchant"}, Review: &ReviewSuggestion{Summary: "Waktu transaksi tidak tertulis.", MissingFields: []string{"transaction_at", "merchant"}, SuggestedActions: []string{"USE_EMAIL_RECEIVED_AT", "ENTER_TRANSACTION_TIME", "IGNORE"}}}
	if !applyEmailReceivedTimeFallback(&extraction, receivedAt) {
		t.Fatal("expected received email time fallback")
	}
	if extraction.TransactionAt == nil || !extraction.TransactionAt.Equal(receivedAt) || extraction.TransactionAtSource != "EMAIL_RECEIVED_AT" {
		t.Fatalf("fallback=%+v", extraction)
	}
	if missing(extraction, "transaction_at") || !missing(extraction, "merchant") {
		t.Fatalf("missing fields=%v", extraction.MissingFields)
	}
	if extraction.Review == nil || len(extraction.Review.MissingFields) != 1 || extraction.Review.MissingFields[0] != "merchant" {
		t.Fatalf("review missing fields=%+v", extraction.Review)
	}
	if len(extraction.Review.SuggestedActions) != 1 || extraction.Review.SuggestedActions[0] != "IGNORE" {
		t.Fatalf("review actions=%v", extraction.Review.SuggestedActions)
	}
	if extraction.Review.Summary != "Waktu transaksi menggunakan waktu penerimaan email. Lengkapi detail transaksi yang masih diperlukan." {
		t.Fatalf("review summary=%q", extraction.Review.Summary)
	}
	if applyEmailReceivedTimeFallback(&extraction, receivedAt) {
		t.Fatal("existing transaction time must not be replaced")
	}
	if applyEmailReceivedTimeFallback(&Extraction{Kind: "NON_TRANSACTION", AmountIDR: ptr("1")}, receivedAt) {
		t.Fatal("non-transaction must not receive fallback")
	}
}
func ptr(v string) *string { return &v }
func timePtr() *time.Time  { v := time.Now(); return &v }

func TestMissingTransactionTimeAloneNeverOpensBankFactsReview(t *testing.T) {
	listener := Listener{TrackingPolicy: "SPENDING_ONLY"}
	receivedAt := time.Date(2026, 8, 31, 3, 32, 46, 0, time.UTC)
	outgoing, channel, merchant := "OUTGOING", "QR", "GRAB"
	complete := func() Extraction {
		return Extraction{Kind: "TRANSACTION", Direction: &outgoing, Channel: &channel, Merchant: &merchant, AmountIDR: ptr("13120"), MissingFields: []string{"transaction_at"}}
	}

	extraction := complete()
	if !applyEmailReceivedTimeFallback(&extraction, receivedAt) {
		t.Fatal("received time must settle a missing transaction time")
	}
	if extraction.TransactionAtSource != "EMAIL_RECEIVED_AT" || !extraction.TransactionAt.Equal(receivedAt) {
		t.Fatalf("provenance not recorded: %v %q", extraction.TransactionAt, extraction.TransactionAtSource)
	}
	result := EvaluateBankEmail(listener, extraction, nil)
	if result.ReviewType == "UNKNOWN_BANK_TEMPLATE" || result.ReviewType == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" {
		t.Fatalf("missing time alone opened a bank-facts review: %+v", result)
	}
	if result.Type != "EXPENSE" {
		t.Fatalf("expected an expense decision, got %+v", result)
	}

	noAmount := complete()
	noAmount.AmountIDR = nil
	if applyEmailReceivedTimeFallback(&noAmount, receivedAt) {
		t.Fatal("received time must not paper over a missing amount")
	}
	if result := EvaluateBankEmail(listener, noAmount, nil); result.ReviewType != "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" {
		t.Fatalf("missing amount must stay a review: %+v", result)
	}
	unknown := complete()
	unknown.Kind = "UNKNOWN"
	if applyEmailReceivedTimeFallback(&unknown, receivedAt) {
		t.Fatal("received time must not settle an unknown notification kind")
	}
	if result := EvaluateBankEmail(listener, unknown, nil); result.ReviewType != "UNKNOWN_BANK_TEMPLATE" {
		t.Fatalf("unknown kind must keep UNKNOWN_BANK_TEMPLATE: %+v", result)
	}
	if applyEmailReceivedTimeFallback(&Extraction{Kind: "TRANSACTION", AmountIDR: ptr("1")}, time.Time{}) {
		t.Fatal("a zero received time is not evidence")
	}
}

func TestOutgoingTransferToRememberedMerchantIsTheRememberedExpense(t *testing.T) {
	listener := Listener{TrackingPolicy: "SPENDING_ONLY"}
	outgoing, transfer, merchant, account := "OUTGOING", "TRANSFER", "PAMELLA DUA", "1234567890"
	extraction := func(name *string) Extraction {
		return Extraction{Kind: "TRANSACTION", Direction: &outgoing, Channel: &transfer, Merchant: name, Counterparty: &account, AmountIDR: ptr("105226"), TransactionAt: timePtr()}
	}
	remembered := MerchantMemory{MerchantID: "merchant-1", CategoryID: "cat-groceries", AutoApply: true}
	ownAccount := []KnownAccount{{MatchHint: "7890", Relationship: "OWN_ACCOUNT"}}

	cases := []struct {
		name       string
		extraction Extraction
		known      []KnownAccount
		memory     MerchantMemory
		want       PolicyResult
	}{
		{"remembered merchant settles the transfer label", extraction(&merchant), nil, remembered, PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", CategoryID: "cat-groceries", Description: "Pengeluaran dengan kategori merchant yang telah dikonfirmasi.", AutoConfirm: true}},
		{"a known own account still wins over memory", extraction(&merchant), ownAccount, remembered, PolicyResult{Type: "TRANSFER", Status: "CONFIRMED", Description: "Transfer antar rekening yang dikenal.", AutoConfirm: true}},
		{"no memory keeps the human transfer choice", extraction(&merchant), nil, MerchantMemory{}, review("Tujuan transfer perlu dikonfirmasi.", "TRANSFER_CLASSIFICATION")},
		{"a disabled alias keeps the human transfer choice", extraction(&merchant), nil, MerchantMemory{MerchantID: "merchant-1", CategoryID: "cat-groceries"}, review("Tujuan transfer perlu dikonfirmasi.", "TRANSFER_CLASSIFICATION")},
		{"memory without an observed merchant keeps the human transfer choice", extraction(nil), nil, remembered, review("Tujuan transfer perlu dikonfirmasi.", "TRANSFER_CLASSIFICATION")},
		{"a known account without hint digits never matches", extraction(nil), []KnownAccount{{MatchHint: "rekening utama", Relationship: "OWN_ACCOUNT"}}, MerchantMemory{}, review("Tujuan transfer perlu dikonfirmasi.", "TRANSFER_CLASSIFICATION")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateBankEmail(listener, tc.extraction, tc.known, tc.memory); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if parked := applyCategoryAutoConfirmSwitch(EvaluateBankEmail(listener, extraction(&merchant), nil, remembered), false); parked.AutoConfirm || parked.ReviewType != "AMBIGUOUS_CATEGORY" || parked.Type != "EXPENSE" {
		t.Fatalf("the category auto-confirm switch must still park a remembered transfer as a category review: %+v", parked)
	}
}
