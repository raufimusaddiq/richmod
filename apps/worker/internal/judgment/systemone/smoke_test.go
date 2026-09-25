package systemone

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// TestRealLiteRouterSystemOneSmoke is the opt-in end-to-end residual-category
// canary: Richmod -> LiteRouter /systemone -> TypeSafe -> LiteRouter pass-through
// -> Richmod decoder. The mocked tests above prove transport behaviour against a
// controlled server; this one also checks a real bounded category decision from
// source evidence and captures its actual versioned model id.
//
// It is skipped unless a secret-bearing environment supplies a real LiteRouter
// endpoint and client key, because it consumes live provider credits:
//
//	SYSTEMONE_SMOKE_BASE_URL=<literouter base url>
//	SYSTEMONE_SMOKE_LITEROUTER_KEY=<literouter client key>
//	SYSTEMONE_SMOKE_MODEL=<model id>   # optional, defaults to typesafe/jev-latest
//
// Point BASE_URL at LiteRouter's internal address on a shared Docker network
// (http://9router:20128/v1), not a public hostname. The worker reaches LiteRouter
// directly over the container network by design: routing a bounded decision out
// through the public edge and back into the same host would add a round trip and
// a failure surface for no benefit. That name resolves only inside those
// networks, so run this from a container alongside LiteRouter.
//
// The asserted model id is the *concrete* version the alias resolved to (for
// example jev-1.13.0). typesafe/jev-latest is deliberately an alias; the concrete
// version is what belongs on a stored judgment_decision row so a decision stays
// reproducible after the alias advances (PRD §18).
//
// Only the LiteRouter client key is read here. No upstream provider key (for
// example a TypeSafe key) is configured in Richmod, by design.
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
	// The real provider must return its actual versioned model id so a stored
	// decision stays attributable to the model that produced it (PRD §18).
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
