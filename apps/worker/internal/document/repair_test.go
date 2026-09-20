package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type repairGateway struct {
	call     gateway.ToolCall
	required bool
	tools    []gateway.ToolDefinition
	content  any
	prompt   string
}

func (g *repairGateway) NativeToolCall(_ context.Context, _ string, prompt string, content any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) != 1 || tools[0].Name != "repair_receipt_fields" {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected repair tools")
	}
	g.required = len(options) == 1 && options[0].Required
	g.tools = tools
	g.content = content
	g.prompt = prompt
	return g.call, gateway.Metadata{Model: "vision-model"}, nil
}

// Repair must re-read the document: a flagged amount cannot be corrected from
// an empty prompt with no evidence attached.
func TestRepairSendsDocumentEvidenceAndFlaggedFields(t *testing.T) {
	value := receiptExtraction{Merchant: "Solaria", Currency: "IDR", Total: "6500", Confidence: 0.9}
	issues := validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}}
	llm := &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"65000"}}`)}}
	content := []map[string]any{{"type": "input_text", "text": "Extract this receipt."}, {"type": "input_image", "image_url": "data:image/jpeg;base64,AA=="}}
	if _, _, err := repairExtracted(context.Background(), llm, "doc-evidence", "RECEIPT", content, &value, &issues, func(receiptExtraction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if llm.content == nil {
		t.Fatal("repair call omitted document evidence")
	}
	pages, ok := llm.content.([]map[string]any)
	if !ok || len(pages) != 2 {
		t.Fatalf("repair content = %#v", llm.content)
	}
	for _, want := range []string{"repair_receipt_fields", "total", "INVALID_AMOUNT", "6500"} {
		if !strings.Contains(llm.prompt, want) {
			t.Fatalf("repair prompt missing %q: %s", want, llm.prompt)
		}
	}
	// A repair without evidence must fail closed, never proceed unevidenced.
	issues = validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}}
	unchanged, _, err := repairExtracted(context.Background(), llm, "doc-no-evidence", "RECEIPT", nil, &value, &issues, func(receiptExtraction) error { return nil })
	if err != nil || unchanged.Total != value.Total || !issues.has("", repairFailedCode) {
		t.Fatalf("unevidenced repair was not rejected: value=%+v issues=%s", unchanged, issues.String())
	}
}

func TestRepairUpdatesOnlyRequestedFieldsAndRevalidates(t *testing.T) {
	value := receiptExtraction{Merchant: "Solaria", Currency: "IDR", Total: "65000", Confidence: 0.9}
	issues := validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}, {Field: "merchant", Code: "TEXT_TOO_LONG"}}
	llm := &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"65000","unrequested":"x"}}`)}}
	got, _, err := repairExtracted(context.Background(), llm, "doc-1", "RECEIPT", repairTestContent(), &value, &issues, func(receiptExtraction) error { return nil })
	if err != nil || got.Total != value.Total || !issues.has("", repairFailedCode) {
		t.Fatal("failed repair did not preserve original invalid result")
	}
	issues = validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}}
	llm = &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"95000"}}`)}}
	patched, metadata, err := repairExtracted(context.Background(), llm, "doc-2", "RECEIPT", repairTestContent(), &value, &issues, func(v receiptExtraction) error {
		if v.Total != "95000" || v.Merchant != "Solaria" {
			return fmt.Errorf("patch changed unflagged fields")
		}
		return nil
	})
	if err != nil || !llm.required || metadata.Model != "vision-model" {
		t.Fatalf("patch=%+v required=%t err=%v", patched, llm.required, err)
	}
	// Revalidate failure keeps the original value.
	llm = &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"95000"}}`)}}
	issues = validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}}
	if got, _, err := repairExtracted(context.Background(), llm, "doc-3", "RECEIPT", repairTestContent(), &value, &issues, func(receiptExtraction) error { return fmt.Errorf("still invalid") }); err != nil || got.Total != value.Total || !issues.has("", repairFailedCode) {
		t.Fatal("failed revalidation did not preserve invalid result")
	}
}

// repairTestContent is the document evidence every successful repair needs.
func repairTestContent() []map[string]any {
	return []map[string]any{{"type": "input_text", "text": "Extract this receipt."}, {"type": "input_image", "image_url": "data:image/jpeg;base64,AA=="}}
}

func TestReceiptValidationIssuesMapFieldCodes(t *testing.T) {
	received := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	_, issues := validateReceiptIssues(receiptExtraction{Currency: "USD", Total: "12.5", Confidence: 2}, received)
	if !issues.has("currency", "INVALID_CURRENCY") || !issues.has("total", "INVALID_AMOUNT") || !issues.has("confidence", "INVALID_CONFIDENCE") {
		t.Fatalf("issues = %s", issues.String())
	}
	good := receiptExtraction{Currency: "IDR", Total: "65000", Confidence: 0.9}
	if _, issues := validateReceiptIssues(good, received); len(issues) != 0 {
		t.Fatalf("issues = %s", issues.String())
	}
}

func TestRepairIsBoundedToOneAttempt(t *testing.T) {
	if maxRepairAttempts != 1 {
		t.Fatal("repair must stay bounded to one attempt")
	}
}

func TestRepairIssueFieldsMapToTopLevelAllowlist(t *testing.T) {
	for _, test := range []struct{ doc, issue, want string }{
		{"RECEIPT", "items[2].amount", "items"},
		{"PAYSLIP", "allowances[0].amount", "allowances"},
		{"PAYSLIP", "deductions[1].amount", "deductions"},
		{"RECEIPT", "unknown", ""},
	} {
		if got := repairableTopLevelField(test.doc, test.issue); got != test.want {
			t.Errorf("repairableTopLevelField(%q,%q)=%q, want %q", test.doc, test.issue, got, test.want)
		}
	}
}
