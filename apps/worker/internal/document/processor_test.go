package document

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type classificationGateway struct {
	call    gateway.ToolCall
	options gateway.NativeToolOptions
}

func (g *classificationGateway) Structured(context.Context, string, string, string, any, map[string]any, any) (gateway.Metadata, error) {
	return gateway.Metadata{}, fmt.Errorf("structured output must not be used for document classification")
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
