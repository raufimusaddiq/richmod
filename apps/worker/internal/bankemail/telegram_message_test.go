package bankemail

import (
	"testing"
	"time"
)

func TestBankReviewMessageKeepsKnownFactsForMerchantReview(t *testing.T) {
	at := time.Date(2026, time.September, 2, 2, 24, 0, 0, time.UTC)
	got := bankReviewMessage("UNKNOWN_MERCHANT", "18502", at, "Pembayaran kartu debit")
	want := "Nominal: Rp18.502\nWaktu: 02/09/2026 09:24 WIB\nKeterangan: Pembayaran kartu debit"
	if got != want {
		t.Fatalf("message=%q", got)
	}
}

func TestBankReviewMessageKeepsCategoryPrompt(t *testing.T) {
	at := time.Date(2026, time.September, 2, 2, 24, 0, 0, time.UTC)
	got := bankReviewMessage("AMBIGUOUS_CATEGORY", "4000", at, "Pembayaran toko")
	if got != "🏦 Transaksi bank perlu ditinjau\n\nNominal: Rp4.000\nWaktu: 02/09/2026 09:24 WIB\nKeterangan: Pembayaran toko\n\nPilih kategori atau lengkapi detail transaksi." {
		t.Fatalf("message=%q", got)
	}
}
