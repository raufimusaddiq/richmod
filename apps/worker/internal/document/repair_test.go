package document

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type repairGateway struct {
	call     gateway.ToolCall
	required bool
	tools    []gateway.ToolDefinition
	content  any
}

func (g *repairGateway) NativeToolCall(_ context.Context, _ string, _ string, content any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) != 1 || tools[0].Name != "repair_receipt_fields" {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected repair tools")
	}
	g.required = len(options) == 1 && options[0].Required
	g.tools = tools
	g.content = content
	return g.call, gateway.Metadata{Model: "vision-model"}, nil
}

func TestRepairUpdatesOnlyRequestedFieldsAndRevalidates(t *testing.T) {
	value := receiptExtraction{Merchant: "Solaria", Currency: "IDR", Total: "65000", Confidence: 0.9}
	issues := validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}, {Field: "merchant", Code: "TEXT_TOO_LONG"}}
	llm := &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"65000","unrequested":"x"}}`)}}
	if _, _, err := repairExtracted(context.Background(), llm, "doc-1", "RECEIPT", &value, issues, func(receiptExtraction) error { return nil }); err == nil {
		t.Fatal("accepted unrequested field")
	}
	llm = &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"95000"}}`)}}
	patched, metadata, err := repairExtracted(context.Background(), llm, "doc-1", "RECEIPT", &value, issues, func(v receiptExtraction) error {
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
	if _, _, err := repairExtracted(context.Background(), llm, "doc-1", "RECEIPT", &value, issues, func(receiptExtraction) error { return fmt.Errorf("still invalid") }); err == nil {
		t.Fatal("accepted failed revalidation")
	}
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
