package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

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
