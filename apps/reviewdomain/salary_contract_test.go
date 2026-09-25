package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// Web and both Telegram confirm lanes must record salary through the shared
// operation; no adapter may keep its own salary_source/salary_event mutation SQL.
func TestSalaryRecordingIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/canonical.go",
		"../worker/internal/telegram/review.go",
		"../worker/internal/telegram/agent_review_mutations.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if !strings.Contains(text, "reviewdomain.RecordSalaryEvent") && !strings.Contains(text, "RecordSalaryEvent(") {
			t.Fatalf("%s does not call the shared salary operation", path)
		}
		if strings.Contains(text, "INSERT INTO salary_event") || strings.Contains(text, "INSERT INTO salary_source") {
			t.Fatalf("%s still owns salary mutation SQL", path)
		}
	}
}

// The document promotion step is likewise shared, so a future rename or filter
// change cannot drift between Web and Telegram.
func TestDocumentPromotionIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../worker/internal/telegram/review.go",
		"../worker/internal/telegram/agent_review_mutations.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if !strings.Contains(text, "PromoteEvidenceDocuments") {
			t.Fatalf("%s does not call the shared document promotion", path)
		}
		if strings.Contains(text, "UPDATE document d SET status='EXTRACTED'") {
			t.Fatalf("%s still owns document promotion SQL", path)
		}
	}
}
