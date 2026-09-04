package document

import (
	"testing"
	"time"
)

func TestValidateScreenshotKeepsRowsIndependent(t *testing.T) {
	received := time.Date(2026, 8, 25, 18, 0, 0, 0, jakarta())
	outAt := "2026-08-25T10:00:00+07:00"
	inAt := "2026-08-25T11:00:00+07:00"
	food := "food-dining"
	input := screenshotExtraction{Confidence: .96, Transactions: []screenshotRow{
		{Direction: "OUT", Amount: "75000", Currency: "IDR", TransactionAt: &outAt, Merchant: "Warung", CategorySlug: &food, CategoryConfidence: .94, Confidence: .97},
		{Direction: "IN", Amount: "100000", Currency: "IDR", TransactionAt: &inAt, Merchant: "Teman", CategoryConfidence: 0, Confidence: .92},
	}}
	rows, err := validateScreenshot(input, received, []categoryOption{{ID: "category-id", Slug: food}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Type != "EXPENSE" || rows[1].Type != "INCOME" || rows[0].CategoryID == nil || rows[1].CategoryID != nil {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestValidateScreenshotRejectsOneInvalidRowInsteadOfDroppingIt(t *testing.T) {
	input := screenshotExtraction{Confidence: .9, Transactions: []screenshotRow{{Direction: "OUT", Amount: "10.5", Currency: "IDR", Confidence: .9}}}
	if _, err := validateScreenshot(input, time.Now().In(jakarta()), nil, ""); err == nil {
		t.Fatal("expected the extraction to enter review")
	}
}

func TestSupportedScreenshotTypes(t *testing.T) {
	for _, value := range []string{"BANK_TRANSACTION_SCREENSHOT", "EWALLET_SCREENSHOT", "TRANSACTION_HISTORY_SCREENSHOT", "TRANSFER_PROOF", "BILL_OR_INVOICE"} {
		if !screenshotType(value) {
			t.Fatalf("expected supported type %s", value)
		}
	}
}

func TestValidateInvoiceRequiresPaidStatus(t *testing.T) {
	input := screenshotExtraction{AccountHint: "wallet", PaymentStatus: "UNPAID", Confidence: 1, Transactions: []screenshotRow{{Direction: "OUT", Amount: "50000", Currency: "IDR", Merchant: "PLN", Description: "listrik", Confidence: 1}}}
	if _, err := validateScreenshot(input, time.Now().In(jakarta()), nil, "BILL_OR_INVOICE"); err == nil {
		t.Fatal("expected unpaid invoice to require review")
	}
}

func TestIncomingScreenshotRowsRemainIncomeCandidates(t *testing.T) {
	input := screenshotExtraction{AccountHint: "Primary bank", Confidence: .95, Transactions: []screenshotRow{{Direction: "IN", Amount: "100000", Currency: "IDR", Confidence: .95}}}
	rows, err := validateScreenshot(input, time.Now().In(jakarta()), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Type != "INCOME" {
		t.Fatalf("incoming row must remain an income candidate: %+v", rows)
	}
}
