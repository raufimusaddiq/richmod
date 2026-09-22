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
	criteria := map[string]any{"READ_WEALTH": "net worth", "OTHER_OR_UNCLEAR": "no safe route"}
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
		question := payload["questions"].(map[string]any)["route"].(map[string]any)
		if question["criteria"].(map[string]any)["READ_WEALTH"] != "net worth" {
			t.Fatalf("choice criteria not sent natively: %v", question)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev","answers":{"route":{"type":"choice","choice":"READ_WEALTH","probabilities":{"READ_WEALTH":0.97,"OTHER_OR_UNCLEAR":0.03},"confidence":0.95}}}`))
	}))
	defer server.Close()
	client := New(server.URL, "key", "typesafe/jev-latest", time.Second)
	result, err := client.Evaluate(context.Background(), "req-1", judgment.Request{State: map[string]any{"text": "saldo"}, Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose", Criteria: criteria}}})
	if err != nil {
		t.Fatal(err)
	}
	answer := result.Answers["route"]
	if result.Model != "jev" || answer.Choice != "READ_WEALTH" || answer.Probability != 0.97 || !answer.HasConfidence || answer.Confidence != 0.95 || len(answer.Distribution) != 2 {
		t.Fatalf("result=%+v", result)
	}
	if !judgment.AcceptChoice(answer, criteria, judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.80}) {
		t.Fatal("native choice answer should pass strict acceptance")
	}
}

func TestEvaluateDecodesNoulAndScorePrimitives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-2026-09","answers":{"consent":{"type":"noul","noul":0.92},"urgency":{"type":"score","score":7,"legend":["low","high"],"probabilities":{"low":0.1,"high":0.9},"confidence":0.8}}}`))
	}))
	defer server.Close()
	result, err := New(server.URL, "key", "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{
		"consent": {Type: "noul", Instructions: "consent?"},
		"urgency": {Type: "score", Instructions: "how urgent?", Criteria: judgment.ScoreCriteria{"low", "high"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Answers["consent"].HasNoul || result.Answers["consent"].Noul != 0.92 {
		t.Fatalf("noul=%+v", result.Answers["consent"])
	}
	if result.Answers["urgency"].Score == nil || *result.Answers["urgency"].Score != 7 || len(result.Answers["urgency"].Legend) != 2 {
		t.Fatalf("score=%+v", result.Answers["urgency"])
	}
	if remember, decided := judgment.AcceptNoul(result.Answers["consent"], judgment.NoulPolicy{High: 0.8, Low: 0.2}); !remember || !decided {
		t.Fatal("decoded noul should be accepted")
	}
	if result.Model != "jev-2026-09" {
		t.Fatalf("actual model version must be captured: %q", result.Model)
	}
}

func TestEvaluateRejectsMissingUnexpectedAndInvalidAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"missing":          `{"answers":{}}`,
		"unexpected":       `{"answers":{"other":{"type":"choice","choice":"X","probabilities":{"X":1}}}}`,
		"invalid":          `{"answers":{"route":{"type":"choice","choice":"X","probabilities":{"X":2,"Y":-1}}}}`,
		"no probabilities": `{"answers":{"route":{"type":"choice","choice":"X"}}}`,
		"null probability": `{"answers":{"route":{"type":"choice","choice":"X","probabilities":{"X":null}}}}`,
		"bad confidence":   `{"answers":{"route":{"type":"choice","choice":"X","probabilities":{"X":1},"confidence":2}}}`,
		"legacy shape":     `{"answers":{"route":{"type":"choice","choice":"X","probability":0.9}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := New(server.URL, "key", "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose", Criteria: map[string]any{"X": "x", "Y": "y"}}}})
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
	_, err := New(server.URL, secret, "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose", Criteria: map[string]any{"X": "x", "Y": "y"}}}})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "body=") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestEvaluateRejectsOversizedResponseAndUnknownFields(t *testing.T) {
	for name, body := range map[string]string{
		"unknown answer field": `{"answers":{"route":{"type":"choice","choice":"READ_WEALTH","probabilities":{"READ_WEALTH":0.9,"OTHER_OR_UNCLEAR":0.1},"secret":"x"}}}`,
		"oversized":            strings.Repeat("x", maxResponseBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := New(server.URL, "key", "model", time.Second).Evaluate(context.Background(), "req", judgment.Request{Questions: map[string]judgment.Question{"route": {Type: "choice", Instructions: "choose", Criteria: map[string]any{"READ_WEALTH": "nw", "OTHER_OR_UNCLEAR": "other"}}}})
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}
