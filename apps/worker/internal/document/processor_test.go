package document

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type classificationGateway struct {
	call    gateway.ToolCall
	options gateway.NativeToolOptions
}

func (g *classificationGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) != 1 || tools[0].Name != "classify_financial_document" {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected document tools")
	}
	if len(options) == 1 {
		g.options = options[0]
	}
	return g.call, gateway.Metadata{Model: "vision-model"}, nil
}

func TestDecodePayload(t *testing.T) {
	payload, err := DecodePayload(json.RawMessage(`{"document_id":"document-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if payload.DocumentID != "document-1" {
		t.Fatalf("document ID = %q", payload.DocumentID)
	}
}

func TestAllowedDocumentTypes(t *testing.T) {
	if !allowedType("PAYSLIP") || !allowedType("RECEIPT") || allowedType("EXECUTABLE") {
		t.Fatal("document type allowlist is incorrect")
	}
}

func TestDocumentClassificationRequiresOneNativeTool(t *testing.T) {
	llm := &classificationGateway{call: gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(`{"document_type":"RECEIPT","confidence":0.98,"reason":"merchant and total are visible"}`)}}
	result, metadata, err := (&Processor{gateway: llm}).classify(context.Background(), "document-1", []map[string]any{{"type": "input_text", "text": "classify"}})
	if err != nil {
		t.Fatal(err)
	}
	if !llm.options.Required || result.DocumentType != "RECEIPT" || metadata.Model != "vision-model" {
		t.Fatalf("options=%+v result=%+v metadata=%+v", llm.options, result, metadata)
	}
}

func TestDocumentClassificationRejectsInvalidNativeArguments(t *testing.T) {
	for _, arguments := range []string{
		`{"document_type":"RECEIPT","confidence":0.98,"reason":"receipt","extra":true}`,
		`{"document_type":"RECEIPT","confidence":0.98,"reason":"receipt"}{}`,
	} {
		llm := &classificationGateway{call: gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(arguments)}}
		if _, _, err := (&Processor{gateway: llm}).classify(context.Background(), "document-1", nil); err == nil {
			t.Fatalf("accepted invalid arguments: %s", arguments)
		}
	}
}

func TestClassificationPromptSeparatesBalanceFromTransactionHistory(t *testing.T) {
	if !strings.Contains(classificationPrompt, "current balance") || !strings.Contains(classificationPrompt, "transaction-history") || !strings.Contains(classificationPrompt, "never a Wealth observation") {
		t.Fatal("classification prompt does not distinguish balances from transaction history")
	}
}

func TestWealthObservationUsesBoundedNativeSchemaWithoutCanonicalIDs(t *testing.T) {
	llm := &wealthObservationGateway{call: gateway.ToolCall{Name: "extract_wealth_observation", Arguments: json.RawMessage(`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":null,"unit":null,"unit_price_idr":null,"observed_date":null,"confidence":0.98}`)}}
	value, _, err := (&Processor{gateway: llm}).extractWealthObservation(context.Background(), "document-1", nil)
	if err != nil || value.ObservedValueIDR != "42700000" || !llm.required {
		t.Fatalf("value=%+v required=%t err=%v", value, llm.required, err)
	}
	encoded, _ := json.Marshal(wealthObservationSchema())
	if string(encoded) == "" || string(encoded) == "null" || string(encoded) == "{}" || string(encoded) == "[]" {
		t.Fatal("wealth observation schema missing")
	}
	if strings.Contains(string(encoded), "wealth_account_id") || strings.Contains(string(encoded), "household_id") || strings.Contains(string(encoded), "transaction_id") {
		t.Fatal("wealth observation schema exposed canonical IDs")
	}
}

func TestWealthObservationRejectsInvalidOptionalNumerics(t *testing.T) {
	for _, arguments := range []string{
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"-1","unit":"unit","unit_price_idr":"1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"abc","unit":"unit","unit_price_idr":"1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"1/2","unit":"unit","unit_price_idr":"1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"1e3","unit":"unit","unit_price_idr":"1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"123456789012345678901","unit":"unit","unit_price_idr":"1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"0.12345678901","unit":"unit","unit_price_idr":"1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"1","unit":"unit","unit_price_idr":"-1000","observed_date":null,"confidence":0.98}`,
		`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":"1","unit":"unit","unit_price_idr":"1.5","observed_date":null,"confidence":0.98}`,
	} {
		llm := &wealthObservationGateway{call: gateway.ToolCall{Name: "extract_wealth_observation", Arguments: json.RawMessage(arguments)}}
		value, _, err := (&Processor{gateway: llm}).extractWealthObservation(context.Background(), "document-1", nil)
		if err != nil {
			continue
		}
		if validOptionalDecimal(value.Quantity) && validOptionalWholeMoney(value.UnitPriceIDR) {
			t.Fatalf("accepted invalid optional numeric fields: %s", arguments)
		}
	}
}

type wealthObservationGateway struct {
	call     gateway.ToolCall
	required bool
}

func (g *wealthObservationGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) != 1 || tools[0].Name != "extract_wealth_observation" {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected wealth observation tool")
	}
	g.required = len(options) == 1 && options[0].Required
	return g.call, gateway.Metadata{}, nil
}

func TestReadDocumentRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-finance-document")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	storage, err := blob.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{storage: storage}
	if _, err := processor.readDocument(context.Background(), "../outside-finance-document"); err == nil {
		t.Fatal("path traversal storage reference was accepted")
	}
}
