package reviewdomain

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Source-event status is derived in exactly one place, RefreshSourceLifecycle
// (with MarkSourceIgnored for a dismissal). Lanes that recomputed it inline with
// their own CASE drifted and left events stuck (migrations 00083/00084).
func TestSourceLifecycleIsDerivedOnlyByTheSharedRule(t *testing.T) {
	// Any CASE that derives processing_status is a second lifecycle rule,
	// including the agent's finalization CASE that kept NEEDS_REVIEW/IGNORED and
	// the card-choice CASE that settled RECEIVED/PROCESSING (agent.go,
	// agent_card_answer.go): both now call finalizeAgentSource, which delegates
	// to RefreshSourceLifecycle.
	inline := regexp.MustCompile(`processing_status\s*=\s*CASE\b`)
	// Settling a review-parked source by hand ("PROCESSED where it was
	// NEEDS_REVIEW") hides its open work; the shared refresh decides that.
	parkedSettle := regexp.MustCompile("(?s)SET\\s+processing_status\\s*=\\s*'PROCESSED'[^`]*processing_status\\s*=\\s*'NEEDS_REVIEW'")
	files := []string{}
	for _, pattern := range []string{"*.go", "../api/internal/*/*.go", "../worker/internal/*/*.go"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	for _, file := range files {
		if filepath.Base(file) == "lifecycle.go" || regexp.MustCompile(`_test\.go$`).MatchString(file) {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if inline.Match(raw) {
			t.Fatalf("%s derives source_event.processing_status inline; call RefreshSourceLifecycle", file)
		}
		if parkedSettle.Match(raw) {
			t.Fatalf("%s settles a NEEDS_REVIEW source directly; call RefreshSourceLifecycle", file)
		}
		// Reopen opens new work and lets the shared refresh derive the status
		// from it; it never writes processing_status itself.
		if filepath.Base(file) == "reopen.go" && regexp.MustCompile(`SET\s+processing_status`).Match(raw) {
			t.Fatalf("%s sets processing_status before the shared refresh; let RefreshSourceLifecycle own it", file)
		}
		// A resolver must not park a document in review unconditionally (T13):
		// the shared lifecycle settles documents. Only document intake in the
		// worker sets NEEDS_REVIEW when it opens a review itself.
		if !regexp.MustCompile(`worker`).MatchString(file) {
			if regexp.MustCompile(`UPDATE document SET status='NEEDS_REVIEW'`).Match(raw) {
				t.Fatalf("%s sets document.status=NEEDS_REVIEW directly; let RefreshSourceLifecycle settle it", file)
			}
		}
	}
}

// The agent lanes finalize their source through the shared lifecycle.
func TestAgentFinalizationRoutesThroughTheSharedLifecycle(t *testing.T) {
	agent := sourceFile(t, "../worker/internal/telegram/agent.go")
	if !regexp.MustCompile(`(?s)func finalizeAgentSource\(.*?reviewdomain\.RefreshSourceLifecycle`).MatchString(agent) {
		t.Fatal("finalizeAgentSource must delegate to reviewdomain.RefreshSourceLifecycle")
	}
	for path, callers := range map[string]int{"../worker/internal/telegram/agent.go": 2, "../worker/internal/telegram/agent_card_answer.go": 1} {
		if got := regexp.MustCompile(`finalizeAgentSource\(ctx, tx,`).FindAllStringIndex(sourceFile(t, path), -1); len(got) < callers {
			t.Fatalf("%s finalizes %d agent sources through the shared rule, want %d", path, len(got), callers)
		}
	}
}

// A dismissed wealth observation is rejected evidence from either origin, the
// same way an accepted one is accepted evidence from either origin.
func TestWealthObservationCountsSymmetricallyInTheLifecycle(t *testing.T) {
	lifecycle := sourceFile(t, "lifecycle.go")
	for _, status := range []string{`wo.status IN ('ACCEPTED','APPLIED')`, `wo.status='DISMISSED'`} {
		at := strings.Index(lifecycle, status)
		if at < 0 {
			t.Fatalf("lifecycle lost the %s wealth clause", status)
		}
		clause := lifecycle[at:]
		clause = clause[:strings.Index(clause, " AS ")]
		if !strings.Contains(clause, "wo.document_id IN") || !strings.Contains(clause, "wo.financial_email_observation_id IN") {
			t.Fatalf("the %s wealth clause must cover document and provider-email origins: %s", status, clause)
		}
	}
}
