package document

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
