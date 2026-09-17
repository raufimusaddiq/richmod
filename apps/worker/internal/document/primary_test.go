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
	llm := &recordingInterpretationGateway{call: gateway.ToolCall{Name: "interpret_receipt", Arguments: json.RawMessage(`{"confidence":0.9}`)}}
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
