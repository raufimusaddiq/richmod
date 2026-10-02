package reviewdomain

import (
	"strings"
	"testing"
)

// Analytics sends the household to the Tindakan tab; the API closes the action
// and the worker records it. All three must keep using the shared constants and
// functions so the count, the link, and the item cannot drift apart again.
func TestFailedSourceFlowStaysShared(t *testing.T) {
	query := sourceFile(t, "analyticscore/query.go")
	if !strings.Contains(query, `facts.block("PROCESSING_INCOMPLETE", processing, nil, "`+FailedSourceInboxLink+`")`) {
		t.Fatalf("analytics must link PROCESSING_INCOMPLETE to %s", FailedSourceInboxLink)
	}

	handler := sourceFile(t, "../api/internal/integrationaction/handler.go")
	for _, want := range []string{"reviewdomain.FailedSourceActionType", "reviewdomain.IgnoreFailedSource"} {
		if !strings.Contains(handler, want) {
			t.Fatalf("the resolve handler must use %s", want)
		}
	}
	if strings.Contains(handler, "UPDATE source_event") {
		t.Fatal("the resolve handler must not own source_event SQL; IgnoreFailedSource does")
	}

	for path, want := range map[string]string{
		"../worker/internal/telegram/agent_terminal.go": "reviewdomain.RecordFailedSourceAction",
		"../worker/internal/bankemail/processor.go":     "reviewdomain.RecordFailedSourceAction",
		"../worker/cmd/worker/main.go":                  "TerminalCallbackFailureTx",
	} {
		body := sourceFile(t, path)
		if !strings.Contains(body, want) {
			t.Fatalf("%s must use %s", path, want)
		}
		if strings.Contains(body, "INSERT INTO integration_action") {
			t.Fatalf("%s must record failed sources through the shared function, not its own SQL", path)
		}
	}
}
