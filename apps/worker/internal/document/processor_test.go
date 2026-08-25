package document

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

func TestReadDocumentRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-finance-document")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	processor := &Processor{root: root}
	if _, err := processor.readDocument("../outside-finance-document"); err == nil {
		t.Fatal("path traversal storage reference was accepted")
	}
}
