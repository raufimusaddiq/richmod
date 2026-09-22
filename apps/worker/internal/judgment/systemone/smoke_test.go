package systemone

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// TestRealLiteRouterSystemOneSmoke is the opt-in end-to-end contract check from
// PRD §7: Richmod -> LiteRouter /systemone -> TypeSafe -> LiteRouter pass-through
// -> Richmod decoder. The mocked tests above prove transport behaviour against a
// controlled server; this one proves the real provider accepts the native
// question schema and that its actual versioned model id is captured.
//
// It is skipped unless a secret-bearing environment supplies a real LiteRouter
// endpoint and client key, because it consumes live provider credits:
//
//	SYSTEMONE_SMOKE_BASE_URL=<literouter base url>
//	SYSTEMONE_SMOKE_LITEROUTER_KEY=<literouter client key>
//	SYSTEMONE_SMOKE_MODEL=<model id>   # optional, defaults to typesafe/jev-latest
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

	criteria := map[string]any{"READ_WEALTH": "net worth of the household", "OTHER_OR_UNCLEAR": "no safe route"}
	client := New(baseURL, clientKey, model, 20*time.Second)
	result, err := client.Evaluate(context.Background(), "richmod-systemone-smoke", judgment.Request{
		State: map[string]any{"user_text": "berapa net worth saya?"},
		Questions: map[string]judgment.Question{
			"route": {Type: "choice", Instructions: "Choose one allowed finance route.", Criteria: criteria},
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
	answer, ok := result.Answers["route"]
	if !ok {
		t.Fatalf("real LiteRouter response omitted the requested question: %+v", result.Answers)
	}
	if !judgment.AcceptChoice(answer, criteria, judgment.ChoicePolicy{MinTop: 0.50, MinMargin: 0.10}) {
		t.Fatalf("real LiteRouter answer did not decode to a usable Choice: %+v", answer)
	}
	if _, known := criteria[answer.Choice]; !known {
		t.Fatalf("real LiteRouter chose an option outside the server criteria: %q", answer.Choice)
	}
	t.Logf("real systemone smoke ok: model=%s choice=%s probability=%.3f", result.Model, answer.Choice, answer.Probability)
}
