package document

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShadowAgreementCounters(t *testing.T) {
	agree, counters := shadowAgreement(Interpretation{DocumentType: "RECEIPT", Confidence: 0.9}, documentClassification{DocumentType: "RECEIPT"})
	if agree != "agree" || len(counters) != 2 {
		t.Fatalf("agree=%s counters=%v", agree, counters)
	}
	disagree, counters := shadowAgreement(Interpretation{DocumentType: "PAYSLIP"}, documentClassification{DocumentType: "RECEIPT"})
	if disagree != "disagree" || counters[0] != "disagree" {
		t.Fatalf("disagree=%s counters=%v", disagree, counters)
	}
	ambiguous := Interpretation{DocumentType: "RECEIPT", Confidence: 0.9, Fields: map[string]ObservedField{"merchant": {Status: ObservationAmbiguous}}}
	if got, _ := shadowAgreement(ambiguous, documentClassification{DocumentType: "RECEIPT"}); got != "malformed" {
		t.Fatalf("ambiguous agreement = %s", got)
	}
}

func TestShadowComparisonPayloadIsRedacted(t *testing.T) {
	shadow := Interpretation{DocumentType: "RECEIPT", Tool: toolInterpretReceipt, Confidence: 0.9, Fields: map[string]ObservedField{
		"merchant": {Value: json.RawMessage(`"Warung Kopi Susu"`), Status: ObservationPresent, Confidence: 0.9},
	}}
	legacy := documentClassification{DocumentType: "RECEIPT", Confidence: 0.95, Reason: "merchant and total visible"}
	payload, err := json.Marshal(shadowComparison{
		ShadowType: shadow.DocumentType, ShadowStatus: shadowStatus(shadow), LegacyType: legacy.DocumentType,
		Agreement: "agree", DocumentType: legacy.DocumentType, Counters: []string{"agree"}, LatencyMS: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, forbidden := range []string{"Warung", "visible", "caption", "file_name", "amount", "total", "household", "document-1", "model"} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("shadow payload leaks %q: %s", forbidden, encoded)
		}
	}
}

func TestShadowErrorClassIsBounded(t *testing.T) {
	for _, test := range []struct{ errType, want string }{
		{"*gateway.ToolCallError", "gateway_tool_call"},
		{"*json.UnmarshalTypeError", "malformed_payload"},
		{"something else", "unknown_error"},
	} {
		if got := shadowErrorClass(test.errType); got != test.want {
			t.Errorf("shadowErrorClass(%q)=%q", test.errType, got)
		}
	}
}

func TestShadowStatusBounded(t *testing.T) {
	if got := shadowStatus(Interpretation{DocumentType: "RECEIPT"}); got != "present" {
		t.Errorf("status=%s", got)
	}
	if got := shadowStatus(Interpretation{DocumentType: "RECEIPT", Confidence: 2}); got != "malformed" {
		t.Errorf("status=%s", got)
	}
	fields := map[string]ObservedField{"merchant": {Status: ObservationMissing}}
	if got := shadowStatus(Interpretation{DocumentType: "RECEIPT", Fields: fields}); got != "missing" {
		t.Errorf("status=%s", got)
	}
}
