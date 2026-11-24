package financialemail

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
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

func TestPreviewFailureMessageNeverLeaksGatewayEndpoint(t *testing.T) {
	leaky := &url.Error{Op: "Post", URL: "https://llm.internal.example:8443/v1/responses", Err: errors.New("dial tcp 10.0.0.7:8443: connection refused")}
	cases := map[string]error{
		"transport":  &gateway.UnavailableError{Err: fmt.Errorf("call LLM tool gateway: %w", leaky)},
		"status":     &gateway.StatusError{StatusCode: 503, Endpoint: "responses"},
		"validation": fmt.Errorf("decode emit arguments: %w", leaky),
	}
	want := map[string]string{"transport": "AI gateway unavailable", "status": "AI gateway unavailable", "validation": "AI extraction failed"}
	for name, cause := range cases {
		got := previewFailureMessage(cause)
		if got != want[name] || strings.Contains(got, "llm.internal") || strings.Contains(got, "10.0.0.7") {
			t.Errorf("%s: previewFailureMessage = %q, want %q", name, got, want[name])
		}
	}
	if previewFailureMessage(nil) != "preview failed" {
		t.Fatal("missing cause keeps the generic message")
	}
}
