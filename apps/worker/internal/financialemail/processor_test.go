package financialemail

import "testing"

func TestMoneyRequiresCanonicalWholeIDR(t *testing.T) {
	for _, value := range []string{"0", "3000000"} {
		if !nonNegativeWholeMoney(&value) {
			t.Fatalf("%q rejected", value)
		}
	}
	for _, value := range []string{"03", "3.0", "-1", "1e3", "abc"} {
		if nonNegativeWholeMoney(&value) {
			t.Fatalf("%q accepted", value)
		}
	}
}

func TestCashMovementRequiresPositiveBoundedIDR(t *testing.T) {
	zero, spacedZero, one, large := "0", " 0 ", "1", "3000000"
	tooLarge, negative, decimal, exponent, empty := "123456789012345678901", "-1", "1.0", "1e3", ""
	if positiveWholeMoney(nil) || positiveWholeMoney(&zero) || positiveWholeMoney(&spacedZero) || !positiveWholeMoney(&one) || !positiveWholeMoney(&large) || positiveWholeMoney(&tooLarge) || positiveWholeMoney(&negative) || positiveWholeMoney(&decimal) || positiveWholeMoney(&exponent) || positiveWholeMoney(&empty) {
		t.Fatal("positive whole money contract")
	}
}

func TestFinancialEmailToolHasOneObservationArray(t *testing.T) {
	definition := tool([]string{"WEALTH_VALUE"})
	if definition.Name != "emit_financial_email_observations" {
		t.Fatal(definition.Name)
	}
	if _, ok := definition.Parameters["properties"].(map[string]any)["observations"]; !ok {
		t.Fatal("observations missing")
	}
}
