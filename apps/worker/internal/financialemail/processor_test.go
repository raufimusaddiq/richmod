package financialemail

import (
	"math"
	"strings"
	"testing"
)

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

func TestValidateOutputBoundsObservationsConfidenceAndText(t *testing.T) {
	good := observation{Kind: "NON_ACTIONABLE", Confidence: 0.5}
	if err := validateOutput(output{Observations: []observation{good}}); err != nil {
		t.Fatal(err)
	}
	tooMany := output{}
	for range maxObservations + 1 {
		tooMany.Observations = append(tooMany.Observations, good)
	}
	long := strings.Repeat("x", maxHintRunes+1)
	cases := map[string]output{
		"too many":    tooMany,
		"negative":    {Observations: []observation{{Kind: "UNKNOWN", Confidence: -0.1}}},
		"above one":   {Observations: []observation{{Kind: "UNKNOWN", Confidence: 1.1}}},
		"nan":         {Observations: []observation{{Kind: "UNKNOWN", Confidence: math.NaN()}}},
		"infinite":    {Observations: []observation{{Kind: "UNKNOWN", Confidence: math.Inf(1)}}},
		"long hint":   {Observations: []observation{{Kind: "UNKNOWN", FundingAccountHint: &long}}},
		"long kind":   {Observations: []observation{{Kind: long}}},
		"long amount": {Observations: []observation{{Kind: "UNKNOWN", AmountIDR: &long}}},
	}
	for name, value := range cases {
		if validateOutput(value) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestObservedDateIsTrimmedBeforeValidation(t *testing.T) {
	padded, bad := " 2026-09-08 ", "08/09/2026"
	if !validObservedDate(&padded) || validObservedDate(&bad) {
		t.Fatal("observed_date must be trimmed and strictly YYYY-MM-DD")
	}
}
