package reviewdomain

import (
	"testing"
	"time"
)

func TestEmailAmountSupportsIndonesianFormatting(t *testing.T) {
	if !EmailAmountSupports("1250000", "Pembayaran Rp1.250.000,00 berhasil", "") {
		t.Fatal("an amount written with thousands separators and decimals must support the extracted amount")
	}
	if EmailAmountSupports("125000000", "Pembayaran Rp1.250.000,00 berhasil") {
		t.Fatal("a 100x model amount must not be supported by the email")
	}
	if !EmailAmountSupports("54000", "Transaksi IDR 54.000") {
		t.Fatal("a currency prefix must not hide the amount")
	}
	if EmailAmountSupports("125000050", "Pembayaran Rp1.250.000,50 berhasil") {
		t.Fatal("a non-zero decimal part must not support a whole IDR amount")
	}
	if EmailAmountSupports("1250000", "Pembayaran Rp1.250.000,50 berhasil") {
		t.Fatal("a non-zero decimal part must not support the integer part")
	}
	if !EmailAmountSupports("1250000", "Pembayaran Rp1.250.000,00 berhasil") {
		t.Fatal("a zero decimal part with an Indonesian separator must support the amount")
	}
	if !EmailAmountSupports("1250000", "Transaksi IDR 1,250,000.00") {
		t.Fatal("an English formatted amount must support the amount")
	}
	if !EmailAmountSupports("50000", "Transaksi Rp50.000") {
		t.Fatal("a three-digit thousand group must not be read as a decimal part")
	}
	if EmailAmountSupports("15", "Transaksi Rp 1.5") {
		t.Fatal("a short non-zero decimal part must not support a whole IDR amount")
	}
}

func TestEmailTimePlausibleRejectsShiftedOffsets(t *testing.T) {
	received := time.Date(2026, 9, 7, 23, 35, 0, 0, time.FixedZone("WIB", 7*60*60))
	shifted := time.Date(2026, 9, 7, 23, 30, 0, 0, time.UTC)
	if EmailTimePlausible(shifted, received) {
		t.Fatal("a UTC timestamp from a WIB email must not be plausible")
	}
	if !EmailTimePlausible(received.Add(10*time.Minute), received) {
		t.Fatal("a transaction time within ten minutes of receipt must be plausible")
	}
	if EmailTimePlausible(received.Add(11*time.Minute), received) {
		t.Fatal("a transaction time beyond ten minutes of receipt must not be plausible")
	}
}
