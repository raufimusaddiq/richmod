package document

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generic document interpretation pipeline must stay decoupled from the
// frozen bank SPENDING_ONLY pipeline and the generic provider email pipeline.
func TestDocumentPackageDoesNotDependOnEmailPipelines(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		if strings.Contains(content, "internal/bankemail") || strings.Contains(content, "internal/financialemail") {
			t.Errorf("%s imports a bank/provider email pipeline", entry.Name())
		}
	}
}
