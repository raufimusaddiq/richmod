package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStructuredUsesNonStreamingResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing gateway bearer authentication")
		}
		if r.Header.Get("X-Request-ID") != "request-1" {
			t.Error("missing request correlation ID")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["stream"] != false {
			t.Fatal("stream must be false")
		}
		if body["model"] != "test-model" {
			t.Errorf("model = %v", body["model"])
		}
		metadata, _ := body["metadata"].(map[string]any)
		if metadata["task"] != "classification" {
			t.Errorf("task metadata = %v", metadata["task"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","output":[{"type":"message","content":[{"type":"output_text","text":"{\"ok\":true}"}]}],"usage":{"input_tokens":4,"output_tokens":2},"cost":"0"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/v1", "secret", map[string]string{"finance.test": "test-model"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	meta, err := client.Structured(context.Background(), StructuredRequest{
		RequestID:   "request-1",
		Task:        "classification",
		ModelPolicy: "finance.test",
		SchemaName:  "test",
		Schema:      map[string]any{"type": "object"},
	}, &out)
	if err != nil {
		t.Fatalf("Structured() error = %v", err)
	}
	if !out.OK || meta.OutputTokens != 2 || meta.Model != "test-model" {
		t.Fatalf("out = %#v, metadata = %#v", out, meta)
	}
}

func TestStructuredRejectsUnknownOutputFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"{\"ok\":true,\"unexpected\":1}"}]}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret", map[string]string{"finance.test": "test-model"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	_, err = client.Structured(context.Background(), StructuredRequest{
		ModelPolicy: "finance.test",
		SchemaName:  "test",
		Schema:      map[string]any{"type": "object"},
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Structured() error = %v", err)
	}
}
