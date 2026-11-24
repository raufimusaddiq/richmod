package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type repairGateway struct {
	call          gateway.ToolCall
	required      bool
	disableStrict bool
	tools         []gateway.ToolDefinition
	content       any
	prompt        string
}

func (g *repairGateway) NativeToolCall(_ context.Context, _ string, prompt string, content any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) != 1 || tools[0].Name != "repair_receipt_fields" {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected repair tools")
	}
	g.required = len(options) == 1 && options[0].Required
	g.disableStrict = len(options) == 1 && options[0].DisableStrict
	g.tools = tools
	g.content = content
	g.prompt = prompt
	return g.call, gateway.Metadata{Model: "vision-model"}, nil
}

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
	if !ok || len(pages) != 3 {
		t.Fatalf("repair content = %#v", llm.content)
	}
	for _, want := range []string{"repair_receipt_fields", "total", "INVALID_AMOUNT"} {
		if !strings.Contains(llm.prompt, want) {
			t.Fatalf("repair prompt missing %q: %s", want, llm.prompt)
		}
	}
	if strings.Contains(llm.prompt, "6500") || strings.Contains(llm.prompt, "Solaria") {
		t.Fatalf("untrusted extraction leaked into the system prompt: %s", llm.prompt)
	}
	extraction, _ := pages[2]["text"].(string)
	if !strings.Contains(extraction, "<untrusted_extraction_json>") || !strings.Contains(extraction, "6500") {
		t.Fatalf("extraction JSON was not wrapped as untrusted user content: %q", extraction)
	}
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
	if err != nil || !llm.required || !llm.disableStrict || metadata.Model != "vision-model" {
		t.Fatalf("patch=%+v required=%t disableStrict=%t err=%v", patched, llm.required, llm.disableStrict, err)
	}
	llm = &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(`{"fields":{"total":"95000"}}`)}}
	issues = validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}}
	if got, _, err := repairExtracted(context.Background(), llm, "doc-3", "RECEIPT", repairTestContent(), &value, &issues, func(receiptExtraction) error { return fmt.Errorf("still invalid") }); err != nil || got.Total != value.Total || !issues.has("", repairFailedCode) {
		t.Fatal("failed revalidation did not preserve invalid result")
	}
}

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

type failingRepairGateway struct{ err error }

func (g failingRepairGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, g.err
}

func TestRepairPropagatesGatewayUnavailabilityInsteadOfFailingPermanently(t *testing.T) {
	value := receiptExtraction{Merchant: "Solaria", Currency: "IDR", Total: "x", Confidence: .9}
	for _, test := range []struct {
		name        string
		err         error
		wantErr     bool
		wantFlagged bool
	}{
		{"transport", &gateway.UnavailableError{Err: fmt.Errorf("connection reset")}, true, false},
		{"rate limited", &gateway.StatusError{StatusCode: 429}, true, false},
		{"server error", &gateway.StatusError{StatusCode: 503}, true, false},
		{"rejected", &gateway.StatusError{StatusCode: 400}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues := validationIssues{{Field: "total", Code: "INVALID_AMOUNT"}}
			_, _, err := repairExtracted(context.Background(), failingRepairGateway{err: test.err}, "doc", "RECEIPT", repairTestContent(), &value, &issues, func(receiptExtraction) error { return nil })
			if (err != nil) != test.wantErr || issues.has("", repairFailedCode) != test.wantFlagged {
				t.Fatalf("err=%v issues=%s", err, issues.String())
			}
		})
	}
}

func repairWith(t *testing.T, value receiptExtraction, issues validationIssues, arguments string) (receiptExtraction, validationIssues) {
	t.Helper()
	llm := &repairGateway{call: gateway.ToolCall{Name: "repair_receipt_fields", Arguments: json.RawMessage(arguments)}}
	patched, _, err := repairExtracted(context.Background(), llm, "doc", "RECEIPT", repairTestContent(), &value, &issues, func(receiptExtraction) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return patched, issues
}

func TestRepairArrayPatchMayNotTouchUnflaggedRows(t *testing.T) {
	value := receiptExtraction{Merchant: "Solaria", Currency: "IDR", Total: "65000", Confidence: .9, Items: []receiptItem{{Name: "Nasi", Amount: "x"}, {Name: "Teh", Amount: "5000"}}}
	flagged := validationIssues{{Field: "items[0].amount", Code: "INVALID_AMOUNT"}}
	patched, issues := repairWith(t, value, flagged, `{"fields":{"items":[{"name":"Nasi","quantity":null,"amount":"60000"},{"name":"Teh","quantity":null,"amount":"5000"}]}}`)
	if issues.has("", repairFailedCode) || patched.Items[0].Amount != "60000" {
		t.Fatalf("a repair confined to the flagged row was rejected: %s", issues.String())
	}
	for name, arguments := range map[string]string{
		"mutated unflagged row": `{"fields":{"items":[{"name":"Nasi","quantity":null,"amount":"60000"},{"name":"Teh","quantity":null,"amount":"1"}]}}`,
		"dropped row":           `{"fields":{"items":[{"name":"Nasi","quantity":null,"amount":"60000"}]}}`,
		"added row":             `{"fields":{"items":[{"name":"Nasi","quantity":null,"amount":"60000"},{"name":"Teh","quantity":null,"amount":"5000"},{"name":"Es","quantity":null,"amount":"1000"}]}}`,
	} {
		patched, issues := repairWith(t, value, validationIssues{{Field: "items[0].amount", Code: "INVALID_AMOUNT"}}, arguments)
		if !issues.has("", repairFailedCode) || len(patched.Items) != 2 || patched.Items[0].Amount != "x" {
			t.Fatalf("%s was accepted: %+v %s", name, patched.Items, issues.String())
		}
	}
	whole := validationIssues{{Field: "items", Code: "UNMAPPED_VALIDATION_FAILURE"}}
	patched, issues = repairWith(t, value, whole, `{"fields":{"items":[{"name":"Nasi","quantity":null,"amount":"65000"}]}}`)
	if issues.has("", repairFailedCode) || len(patched.Items) != 1 {
		t.Fatalf("a whole-field repair must be allowed to restructure rows: %s", issues.String())
	}
}

func TestRepairRejectsNullForRequiredFields(t *testing.T) {
	value := receiptExtraction{Merchant: "Solaria", Currency: "IDR", Total: "65000", Confidence: .9, Subtotal: ptr("x")}
	patched, issues := repairWith(t, value, validationIssues{{Field: "merchant", Code: "TEXT_TOO_LONG"}}, `{"fields":{"merchant":null}}`)
	if !issues.has("", repairFailedCode) || patched.Merchant != "Solaria" {
		t.Fatalf("null wiped a required string: %q %s", patched.Merchant, issues.String())
	}
	patched, issues = repairWith(t, value, validationIssues{{Field: "confidence", Code: "INVALID_CONFIDENCE"}}, `{"fields":{"confidence":null}}`)
	if !issues.has("", repairFailedCode) {
		t.Fatalf("null zeroed a required number: %+v", patched)
	}
	patched, issues = repairWith(t, value, validationIssues{{Field: "subtotal", Code: "INVALID_AMOUNT"}}, `{"fields":{"subtotal":null}}`)
	if issues.has("", repairFailedCode) || patched.Subtotal != nil {
		t.Fatalf("null for an optional field must be accepted: %s", issues.String())
	}
}

func TestEncodeRepairContextStaysValidUTF8WhenTruncated(t *testing.T) {
	encoded := encodeRepairContext(map[string]string{"merchant": strings.Repeat("é", 3000)})
	if !utf8.ValidString(encoded) || len(encoded) > repairContextMaxBytes {
		t.Fatalf("encoded context valid=%t len=%d", utf8.ValidString(encoded), len(encoded))
	}
}
