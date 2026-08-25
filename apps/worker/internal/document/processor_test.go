package document

import (
	"encoding/json"
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
