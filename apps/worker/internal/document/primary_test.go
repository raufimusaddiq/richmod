package document

import (
	"context"
	"encoding/json"
	"fmt"
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
	data, _ := json.Marshal(gatewayInterpretation{Fields: fields})
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

func TestInterpretationRejectsMalformedUnknownAndOutOfFamilyPayload(t *testing.T) {
	for _, test := range []struct{ name, args string }{
		{name: "not-a-tool", args: `{}`},
		{name: toolInterpretReceipt, args: `not-json`},
		{name: toolInterpretReceipt, args: `{"confidence":0.9,"fields":{}}`},
	} {
		llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: test.name, Arguments: json.RawMessage(test.args)}}
		if _, _, err := (&Processor{gateway: llm}).Interpret(context.Background(), "doc", "", nil); err == nil {
			t.Errorf("accepted %s args %s", test.name, test.args)
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
