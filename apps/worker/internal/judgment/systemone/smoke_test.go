package systemone

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

func TestRealLiteRouterSystemOneSmoke(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("SYSTEMONE_SMOKE_BASE_URL"))
	clientKey := strings.TrimSpace(os.Getenv("SYSTEMONE_SMOKE_LITEROUTER_KEY"))
	model := strings.TrimSpace(os.Getenv("SYSTEMONE_SMOKE_MODEL"))
	if baseURL == "" || clientKey == "" {
		t.Skip("SYSTEMONE_SMOKE_BASE_URL and SYSTEMONE_SMOKE_LITEROUTER_KEY are required for the real smoke")
	}
	if model == "" {
		model = "typesafe/jev-latest"
	}

	criteria := map[string]any{
		"makanan-minuman": "food, prepared meals, groceries",
		"transportasi":    "transport, parking, transit",
	}
	client := New(baseURL, clientKey, model, 20*time.Second)
	result, err := client.Evaluate(context.Background(), "richmod-systemone-residual-category-smoke", judgment.Request{
		State: map[string]any{
			"source_evidence":     "Receipt from Warung Pagi: nasi padang, Rp 48.000, 2026-09-20.",
			"known_facts":         map[string]any{"merchant": "Warung Pagi", "amount_idr": "48000", "transaction_at": "2026-09-20"},
			"missing_facts":       []string{"category"},
			"decision_provenance": "synthetic residual-category canary; no canonical IDs",
		},
		Questions: map[string]judgment.Question{
			"residual_category": {Type: "choice", Instructions: "Choose the household expense category supported by the receipt evidence. Decide category only; amount, merchant, and date are already known.", Criteria: criteria},
		},
	})
	if err != nil {
		t.Fatalf("real LiteRouter call failed: %v", err)
	}
	if strings.TrimSpace(result.Model) == "" {
		t.Fatal("real LiteRouter response carried no model id")
	}
	answer, ok := result.Answers["residual_category"]
	if !ok {
		t.Fatalf("real LiteRouter response omitted the requested question: %+v", result.Answers)
	}
	if !judgment.AcceptChoice(answer, criteria, judgment.ChoicePolicy{MinTop: 0.50, MinMargin: 0.10}) {
		t.Fatalf("real LiteRouter answer did not decode to a usable Choice: %+v", answer)
	}
	if answer.Choice != "makanan-minuman" {
		t.Fatalf("receipt residual category=%q; want makanan-minuman", answer.Choice)
	}
	t.Logf("real systemone residual-category canary ok: model=%s choice=%s probability=%.3f", result.Model, answer.Choice, answer.Probability)
}
