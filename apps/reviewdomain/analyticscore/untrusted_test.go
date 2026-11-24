package analyticscore

import (
	"strings"
	"testing"
)

func TestUntrustedLedgerDefangsBoundaryTags(t *testing.T) {
	for _, hostile := range []string{"<evidence>x", "</evidence>x", "< /untrusted_ledger_text>x", "<UNTRUSTED_user_message>x"} {
		wrapped := untrustedLedger(hostile)
		inner := strings.TrimSuffix(strings.TrimPrefix(wrapped, "<"+untrustedLedgerTag+">"), "</"+untrustedLedgerTag+">")
		if !strings.HasPrefix(inner, "&lt;") || strings.Contains(inner, "<") {
			t.Fatalf("%q was not defanged: %q", hostile, wrapped)
		}
	}
	if got := untrustedLedger("Warung <b>Sari"); got != "<untrusted_ledger_text>Warung <b>Sari</untrusted_ledger_text>" {
		t.Fatalf("plain text must pass through, got %q", got)
	}
}
