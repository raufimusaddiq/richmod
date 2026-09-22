package telegram

import "testing"

func TestHarvestSimpleTransaction(t *testing.T) {
	tests := []struct {
		text, amount, date, explicit, merchant string
	}{
		{text: "catat makan siang 50rb hari ini", amount: "50000", date: "TODAY", merchant: "makan siang"},
		{text: "beli reksa dana 3 juta kemarin", amount: "3000000", date: "YESTERDAY", merchant: "beli reksa dana"},
		{text: "gaji 8.000.000 tanggal 2026-09-21", amount: "8000000", date: "EXPLICIT", explicit: "2026-09-21", merchant: "gaji tanggal"},
	}
	for _, test := range tests {
		got, ok := harvestSimpleTransaction(test.text)
		if !ok || got.Amount != test.amount || got.DateRef != test.date || got.ExplicitDate != test.explicit || got.Merchant != test.merchant {
			t.Fatalf("harvestSimpleTransaction(%q) = %#v, %v", test.text, got, ok)
		}
	}
	if _, ok := harvestSimpleTransaction("makan 50rb dan parkir 5rb"); ok {
		t.Fatal("multiple amounts must use generative extraction")
	}
}
