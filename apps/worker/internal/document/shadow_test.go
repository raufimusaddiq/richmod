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
	if counters[1] != "shadow_by_document_type.RECEIPT" {
		t.Fatalf("missing per-type counter: %v", counters)
	}
	disagree, counters := shadowAgreement(Interpretation{DocumentType: "PAYSLIP"}, documentClassification{DocumentType: "RECEIPT"})
	if disagree != "disagree" || counters[0] != "disagree" {
		t.Fatalf("disagree=%s counters=%v", disagree, counters)
	}
	if counters[1] != "shadow_by_document_type.RECEIPT" {
		t.Fatalf("disagree counter missing legacy type: %v", counters)
	}
	if _, malformedCounters := shadowAgreement(Interpretation{DocumentType: "RECEIPT", Confidence: 1.4}, documentClassification{DocumentType: "RECEIPT"}); malformedCounters[0] != "malformed" {
		t.Fatalf("malformed counters=%v", malformedCounters)
	}
	mismatched, mismatchCounters := shadowAgreement(Interpretation{DocumentType: "RECEIPT", Confidence: 0.9}, documentClassification{DocumentType: "RECEIPT"})
	if mismatched != "agree" || mismatchCounters[0] != "agree" {
		t.Fatalf("agree=%s counters=%v", mismatched, mismatchCounters)
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

// The metric row must carry bounded agree/disagree/malformed counters per
// document type, an error class, and latency — and nothing else.
func TestShadowMetricRowCarriesOnlyRedactedCounters(t *testing.T) {
	shadow := Interpretation{DocumentType: "RECEIPT", Tool: toolInterpretReceipt, Confidence: 0.91, Quality: QualityClear, Fields: map[string]ObservedField{
		"merchant": {Value: json.RawMessage(`"Warung Kopi Susu"`), Status: ObservationPresent, Confidence: 0.91},
	}}
	legacy := documentClassification{DocumentType: "RECEIPT", Confidence: 0.97, Reason: "total and merchant visible on file struk.jpg"}
	agreement, counters := shadowAgreement(shadow, legacy)
	row := shadowComparison{
		ShadowType: shadow.DocumentType, ShadowStatus: shadowStatus(shadow), LegacyType: legacy.DocumentType,
		Agreement: agreement, DocumentType: legacy.DocumentType, Counters: counters, LatencyMS: 7,
	}
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"shadow_type": true, "shadow_status": true, "legacy_type": true, "agreement": true, "document_type": true, "counters": true, "error_class": true, "latency_ms": true}
	for key := range decoded {
		if !allowed[key] {
			t.Errorf("unexpected shadow metric key %q", key)
		}
	}
	if decoded["agreement"] != "agree" || decoded["document_type"] != "RECEIPT" {
		t.Errorf("metric row = %s", payload)
	}
	countersOut, _ := decoded["counters"].([]any)
	if len(countersOut) != 2 || countersOut[0] != "agree" || countersOut[1] != "shadow_by_document_type.RECEIPT" {
		t.Errorf("counters = %v", countersOut)
	}
	encoded := string(payload)
	for _, forbidden := range []string{"Warung", "Kopi", "struk", "jpg", "visible", "file_name", "caption", "amount", "total", "merchant", "household", "document_id", "model", "prompt"} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("shadow metric leaks %q: %s", forbidden, encoded)
		}
	}
	// Error rows are bounded to an error class plus latency.
	failure, err := json.Marshal(shadowComparison{Agreement: "error", Counters: []string{"error", "error_class.malformed_payload"}, ErrorClass: "malformed_payload", LatencyMS: 3})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(failure), "error_message") {
		t.Errorf("failure row leaks details: %s", failure)
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
