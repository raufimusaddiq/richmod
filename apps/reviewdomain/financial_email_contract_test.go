package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// The Review Inbox financial-email resolution must call the shared operation and
// must not keep its own observation update or alias-learning SQL, so the
// resolved-fact rules stay channel-neutral for the future Telegram surface.
func TestFinancialEmailResolutionIsShared(t *testing.T) {
	path := "../api/internal/review/canonical.go"
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "ResolveFinancialEmailReview") {
		t.Fatalf("%s does not call the shared financial email resolution", path)
	}
	if strings.Contains(text, "UPDATE financial_email_observation SET resolved_account_id") {
		t.Fatalf("%s still owns financial email resolution SQL", path)
	}
	if strings.Contains(text, "INSERT INTO financial_entity_alias") {
		t.Fatalf("%s still owns alias-learning SQL", path)
	}
}

// Web IGNORE on a source-backed transfer classification must delegate to
// ReconcileTransfer, which stamps resolved_at/resolved_by and marks the case
// DISMISSED ("parked, not reconciled"). The observation-only shortcut in
// ResolveFinancialEmailReview is for rows with no source_event to reconcile
// against; letting it swallow source-backed cases wrote RESOLVED with NULL
// resolved_at/by and broke the historical marker contract (PR #214 blocker).
func TestWebTransferIgnoreDelegatesToReconcileTransfer(t *testing.T) {
	path := "../api/internal/review/canonical.go"
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	shortcut := "if in.Action == \"IGNORE\" && kind == \"TRANSFER_CLASSIFICATION\" && financialObservation != nil {"
	if strings.Contains(text, shortcut) {
		t.Fatalf("%s lets the observation-only path swallow source-backed transfer cases; "+
			"the shortcut must require source == nil so ReconcileTransfer owns the case lifecycle", path)
	}
}

// Alias keys must fold Unicode compatibility forms the same way every surface
// does. A full-width hint and its ASCII spelling are the same household alias,
// so losing NFKC here would persist two keys for one account and break lookup.
func TestNormalizeAliasFoldsUnicodeCompatibility(t *testing.T) {
	ascii := normalizeAlias("Bank BCA")
	if got := normalizeAlias("Ｂａｎｋ　ＢＣＡ"); got != ascii {
		t.Fatalf("full-width alias folded to %q, want %q", got, ascii)
	}
	if got := normalizeAlias("  Bank	BCA  "); got != ascii {
		t.Fatalf("whitespace alias folded to %q, want %q", got, ascii)
	}
	if normalizeAlias("") != "" {
		t.Fatal("empty alias must stay empty")
	}
}
