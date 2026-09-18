package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// recordingInterpretationGateway captures the content handed to the model so
// tests can prove the text context is sent exactly once.
type recordingInterpretationGateway struct {
	content []map[string]any
	prompts []string
	call    gateway.ToolCall
}

func (g *recordingInterpretationGateway) NativeToolCall(_ context.Context, _ string, prompt string, content any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) == 1 && tools[0].Name == "classify_financial_document" {
		return gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(`{"document_type":"RECEIPT","confidence":0.9,"reason":"visible"}`)}, gateway.Metadata{}, nil
	}
	if len(tools) != 6 {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected interpretation tools")
	}
	g.prompts = append(g.prompts, prompt)
	pages, _ := content.([]map[string]any)
	g.content = pages
	return g.call, gateway.Metadata{Model: "vision-model"}, nil
}

func TestInterpretationModeSelection(t *testing.T) {
	for _, test := range []struct {
		value string
		want  InterpretationMode
	}{
		{"shadow", InterpretationShadow},
		{"primary", InterpretationLegacy},
		{"legacy", InterpretationLegacy},
		{"", InterpretationLegacy},
		{"experimental", InterpretationLegacy},
	} {
		if got := parseInterpretationMode(test.value); got != test.want {
			t.Errorf("parseInterpretationMode(%q)=%q, want %q", test.value, got, test.want)
		}
	}
}

func TestPrimaryInterpretationSendsTextOnce(t *testing.T) {
	page := receiptPage{raw: []byte{1, 2, 3}, mediaType: "image/jpeg"}
	evidence := EvidenceContext{Pages: []receiptPage{page}, Timezone: householdTimezone, Caption: "struk kopi", FileName: "a.jpg", SourceType: "TELEGRAM_IMAGE"}
	llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: "interpret_receipt", Arguments: typedInterpretationFixture(toolInterpretReceipt)}}
	value, _, err := (&Processor{gateway: llm}).interpretWithPrompt(context.Background(), "doc-1", evidence)
	if err != nil {
		t.Fatal(err)
	}
	if value.DocumentType != "RECEIPT" || len(llm.prompts) != 1 {
		t.Fatalf("value=%+v prompts=%d", value, len(llm.prompts))
	}
	for _, item := range llm.content {
		if item["type"] != "input_image" {
			t.Fatalf("interpretation content carried non-image item: %+v", item)
		}
	}
	if len(llm.content) != 1 {
		t.Fatalf("evidence pages = %d", len(llm.content))
	}
}

func typedInterpretationFixture(tool string) json.RawMessage {
	fields := map[string]ObservedField{}
	for name, kind := range interpretationFieldTypes[tool] {
		value := json.RawMessage(`"visible"`)
		if kind == "array" {
			value = json.RawMessage(`[]`)
		}
		fields[name] = ObservedField{Value: value, Status: ObservationPresent, Confidence: 0.9}
	}
	confidence := map[string]float64{}
	for name := range interpretationCriticalFields[tool] {
		confidence[name] = 0.9
	}
	data, _ := json.Marshal(gatewayInterpretation{DocumentTypeConfidence: 0.9, Quality: QualityClear, Fields: fields, FieldConfidence: confidence, MissingFields: []string{}, AmbiguousFields: []string{}})
	return data
}

func TestInterpretationDecodesAllSixTypedTools(t *testing.T) {
	for tool, docType := range interpretationTools {
		llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: tool, Arguments: typedInterpretationFixture(tool)}}
		value, _, err := (&Processor{gateway: llm}).Interpret(context.Background(), "doc", "", nil)
		if err != nil || value.DocumentType != docType || len(value.Fields) != len(interpretationFieldTypes[tool]) {
			t.Fatalf("tool=%s value=%+v err=%v", tool, value, err)
		}
	}
}

func TestW3QualityContractDecodesFailClosedAndRoutesReview(t *testing.T) {
	llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: toolInterpretReceipt, Arguments: typedInterpretationFixture(toolInterpretReceipt)}}
	value, _, err := (&Processor{gateway: llm}).Interpret(context.Background(), "doc", "", nil)
	if err != nil || value.Quality != QualityClear || value.DocumentTypeConf != 0.9 {
		t.Fatalf("value=%+v err=%v", value, err)
	}
	for _, name := range []string{"total", "merchant", "transaction_at"} {
		if value.FieldConfidence[name] != 0.9 {
			t.Fatalf("field_confidence[%s]=%v", name, value.FieldConfidence[name])
		}
	}
	if value.NeedsReview() {
		t.Fatal("clear 0.90 interpretation unexpectedly routed to review")
	}
	low := value
	low.FieldConfidence = map[string]float64{"total": 0.79, "merchant": 0.9, "transaction_at": 0.9}
	if !low.NeedsReview() {
		t.Fatal("low critical-field confidence must route to review")
	}
	degraded := value
	degraded.Quality = QualityDegraded
	if !degraded.NeedsReview() {
		t.Fatal("DEGRADED quality must route to review")
	}
	ambiguous := value
	ambiguous.AmbiguousFields = []string{"merchant"}
	if !ambiguous.NeedsReview() {
		t.Fatal("ambiguous field must route to review")
	}
	for _, quality := range []DocumentQuality{"clear", "", "VALID"} {
		fixture := typedInterpretationFixture(toolInterpretReceipt)
		if quality != "" {
			fixture = json.RawMessage(strings.Replace(string(fixture), `"quality":"CLEAR"`, `"quality":"`+string(quality)+`"`, 1))
		} else {
			fixture = json.RawMessage(strings.Replace(string(fixture), `"quality":"CLEAR",`, ``, 1))
		}
		llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: toolInterpretReceipt, Arguments: fixture}}
		if _, _, err := (&Processor{gateway: llm}).Interpret(context.Background(), "doc", "", nil); err == nil {
			t.Errorf("accepted quality %q", quality)
		}
	}
}

func TestInterpretationRejectsMalformedUnknownAndOutOfFamilyPayload(t *testing.T) {
	for _, test := range []struct{ name, args string }{
		{name: "not-a-tool", args: `{}`},
		{name: toolInterpretReceipt, args: `not-json`},
		{name: toolInterpretReceipt, args: `{"confidence":0.9,"fields":{}}`},
		{name: toolInterpretReceipt, args: strings.Replace(string(typedInterpretationFixture(toolInterpretReceipt)), `"total":{"value":"visible","status":"PRESENT","confidence":0.9}`, `"total":{"value":"visible","status":"PRESENT","confidence":0.9,"extra":true}`, 1)},
		{name: toolInterpretReceipt, args: strings.Replace(string(typedInterpretationFixture(toolInterpretReceipt)), `"total":{"value":"visible","status":"PRESENT","confidence":0.9}`, `"total":{"value":"visible","status":"PRESENT"}`, 1)},
	} {
		llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: test.name, Arguments: json.RawMessage(test.args)}}
		if _, _, err := (&Processor{gateway: llm}).Interpret(context.Background(), "doc", "", nil); err == nil {
			t.Errorf("accepted %s args %s", test.name, test.args)
		}
	}
}

func TestInterpretationRejectsUntrustedConfidenceAndInconsistentLists(t *testing.T) {
	base := typedInterpretationFixture(toolInterpretReceipt)
	for _, mutate := range []func(string) string{
		func(raw string) string { return strings.Replace(raw, `"total":0.9`, `"total":1.1`, 1) },
		func(raw string) string { return strings.Replace(raw, `"merchant":0.9`, `"not_critical":0.9`, 1) },
		func(raw string) string {
			return strings.Replace(raw, `"missing_fields":[]`, `"missing_fields":["merchant"]`, 1)
		},
	} {
		llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: toolInterpretReceipt, Arguments: json.RawMessage(mutate(string(base)))}}
		if _, _, err := (&Processor{gateway: llm}).Interpret(context.Background(), "doc", "", nil); err == nil {
			t.Errorf("accepted malformed interpretation %s", llm.call.Arguments)
		}
	}
}

func TestObservedFieldStatusRoundTrip(t *testing.T) {
	input := gatewayInterpretation{Fields: map[string]ObservedField{"merchant": {Value: json.RawMessage(`null`), Status: ObservationAmbiguous, Confidence: 0.4}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output gatewayInterpretation
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	if output.Fields["merchant"].Status != ObservationAmbiguous || output.Fields["merchant"].Confidence != .4 {
		t.Fatalf("round-trip = %+v", output.Fields["merchant"])
	}
}
