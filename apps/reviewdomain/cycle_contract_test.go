package reviewdomain

import (
	"strings"
	"testing"
)

// Web and the live Telegram lane must resolve a cycle residual through the shared
// operation; no adapter may keep its own basis recomputation or allocation SQL.
func TestCycleResidualIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/canonical.go",
		"../worker/internal/telegram/agent_review_mutations.go",
	} {
		source := mustReadPaths(t, path)
		text := string(source)
		if !strings.Contains(text, "ApplyCycleResidual") {
			t.Fatalf("%s does not call the shared cycle residual operation", path)
		}
		if strings.Contains(text, "INSERT INTO cycle_residual_allocation") || strings.Contains(text, "UPDATE cycle_residual_case SET basis_income_idr") {
			t.Fatalf("%s still owns cycle residual mutation SQL", path)
		}
	}
}
