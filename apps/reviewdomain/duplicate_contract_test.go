package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// Web and Telegram must merge a duplicate through the shared operation; no
// adapter may keep its own void/evidence-copy mutation SQL.
func TestDuplicateMergeIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/handler.go",
		"../worker/internal/telegram/review.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if !strings.Contains(text, "reviewdomain.MergeDuplicateReview") {
			t.Fatalf("%s does not call the shared duplicate merge operation", path)
		}
		if strings.Contains(text, "INSERT INTO reconciliation_merge (") || strings.Contains(text, "INSERT INTO reconciliation_merge(") || strings.Contains(text, "proposal_status='MERGED'") {
			t.Fatalf("%s still owns duplicate merge mutation SQL", path)
		}
	}
}
