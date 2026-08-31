package review

import "testing"

func TestReconciliationScore(t *testing.T) {
	tests := []struct {
		name                        string
		hours                       float64
		merchant, account, category bool
		want                        float64
	}{
		{"strong exact candidate", 0.5, true, true, true, 1.0},
		{"merchant within one day", 12, true, false, false, 0.80},
		{"amount and time only", 0.5, false, false, false, 0.65},
		{"weak three day candidate", 70, false, false, false, 0.50},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := reconciliationScore(test.hours, test.merchant, test.account, test.category); got != test.want {
				t.Fatalf("score = %.2f, want %.2f", got, test.want)
			}
		})
	}
}

func TestReviewMissingFieldsRequiresBankMerchantAndExpenseCategory(t *testing.T) {
	source := "BANK_EMAIL"
	typeValue := item{Type: "EXPENSE", SourceType: &source}
	got := reviewMissingFields(typeValue)
	if len(got) != 2 || got[0] != "merchant" || got[1] != "category" {
		t.Fatalf("missing fields = %#v", got)
	}
}

func TestSameTextNormalizesCaseAndWhitespace(t *testing.T) {
	if !sameText("  PAMELLA   DUA ", "pamella dua") {
		t.Fatal("expected merchant strings to match")
	}
	if sameText("", "pamella dua") {
		t.Fatal("empty merchant must not match")
	}
}
