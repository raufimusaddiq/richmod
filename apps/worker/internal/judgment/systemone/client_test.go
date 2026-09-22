package systemone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

func TestEvaluateUsesNativeSystemOneEndpointAndStrictAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/systemone" || r.Header.Get("Authorization") != "Bearer key" || r.Header.Get("X-Request-ID") != "req-1" {
			t.Fatalf("request path=%s auth=%q id=%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Request-ID"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "typesafe/jev-latest" {
			t.Fatalf("model=%v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev","answers":{"route":{"type":"choice","choice":"READ_WEALTH","probability":0.97}}}`))
	}))
	defer server.Close()
	client := New(server.URL, "key", "typesafe/jev-latest", time.Second)
	result, err := client.Evaluate(context.Background(), "req-1", judgment.Request{State: map[string]any{"text": "saldo"}, Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "jev" || result.Answers["route"].Choice != "READ_WEALTH" || result.Answers["route"].Probability != 0.97 {
		t.Fatalf("result=%+v", result)
	}
}

func TestEvaluateRejectsMissingUnexpectedAndInvalidAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"missing":    `{"answers":{}}`,
		"unexpected": `{"answers":{"other":{"type":"choice","choice":"X","probability":1}}}`,
		"invalid":    `{"answers":{"route":{"type":"choice","choice":"X","probability":2}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := New(server.URL, "key", "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose"}}})
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}

func TestEvaluateNeverLeaksGatewayBodyOrKey(t *testing.T) {
	const secret = "secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "body="+secret, http.StatusBadGateway) }))
	defer server.Close()
	_, err := New(server.URL, secret, "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose"}}})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "body=") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestEvaluateRejectsOversizedResponseAndUnknownFields(t *testing.T) {
	for name, body := range map[string]string{
		"unknown answer field": `{"answers":{"route":{"type":"choice","choice":"READ_WEALTH","probability":0.9,"secret":"x"}}}`,
		"oversized":            strings.Repeat("x", maxResponseBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := New(server.URL, "key", "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose"}}})
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}
