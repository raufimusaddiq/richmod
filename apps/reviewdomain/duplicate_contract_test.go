package reviewdomain

import (
	"strings"
	"testing"
)

func TestDuplicateMergeIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/handler.go",
		telegramReviewSourceGlob,
	} {
		text := string(mustReadPaths(t, path))
		if !strings.Contains(text, "reviewdomain.MergeDuplicateReview") {
			t.Fatalf("%s does not call the shared duplicate merge operation", path)
		}
		if strings.Contains(text, "INSERT INTO reconciliation_merge (") || strings.Contains(text, "INSERT INTO reconciliation_merge(") || strings.Contains(text, "proposal_status='MERGED'") {
			t.Fatalf("%s still owns duplicate merge mutation SQL", path)
		}
	}
}
