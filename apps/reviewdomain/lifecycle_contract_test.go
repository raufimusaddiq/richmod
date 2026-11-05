package reviewdomain

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Source-event status is derived in exactly one place, RefreshSourceLifecycle
// (with MarkSourceIgnored for a dismissal). Lanes that recomputed it inline with
// their own CASE drifted and left events stuck (migrations 00083/00084).
func TestSourceLifecycleIsDerivedOnlyByTheSharedRule(t *testing.T) {
	inline := regexp.MustCompile(`processing_status\s*=\s*CASE\s+WHEN\s+(NOT\s+)?EXISTS`)
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
	}
}
