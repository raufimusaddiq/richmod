package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// Web and every Telegram wealth lane must mutate a wealth observation through
// the shared operations; no adapter may keep its own dismiss/resolve/evidence SQL.
func TestWealthObservationIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/canonical.go",
		"../worker/internal/telegram/review.go",
		"../worker/internal/telegram/agent_bound_mutations.go",
		"../worker/internal/telegram/agent_wealth_asset_purchase_tx.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if !strings.Contains(text, "DismissWealthObservation") && !strings.Contains(text, "ResolveWealthObservation") {
			t.Fatalf("%s does not call a shared wealth observation operation", path)
		}
		if strings.Contains(text, "UPDATE wealth_observation SET status='DISMISSED'") || strings.Contains(text, "UPDATE wealth_observation SET resolved_wealth_account_id") {
			t.Fatalf("%s still owns wealth observation mutation SQL", path)
		}
	}
}

// The reclassification evidence update is shared too.
func TestWealthReclassifyIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../worker/internal/telegram/review.go",
		"../worker/internal/telegram/agent_bound_mutations.go",
		"../worker/internal/telegram/agent_wealth_asset_purchase_tx.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if !strings.Contains(text, "ReclassifyWealthEvidence") {
			t.Fatalf("%s does not call the shared wealth evidence reclassification", path)
		}
		if strings.Contains(text, "document_type='TRANSACTION_HISTORY_SCREENSHOT'") {
			t.Fatalf("%s still owns wealth reclassification SQL", path)
		}
	}
}
