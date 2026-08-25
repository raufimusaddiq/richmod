package gateway

import (
	"encoding/json"
	"testing"
)

func TestNormalizeContentSerializesStructuredDataAsText(t *testing.T) {
	content, err := normalizeContent(map[string]any{"message": "makan 50rb"})
	if err != nil {
		t.Fatalf("normalizeContent() error = %v", err)
	}
	text, ok := content.(string)
	if !ok {
		t.Fatalf("content type = %T", content)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil || decoded["message"] != "makan 50rb" {
		t.Fatalf("decoded = %#v, error = %v", decoded, err)
	}
}
