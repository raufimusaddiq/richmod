package bankemail

import (
	"context"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// failingThenRepairGateway returns an invalid timestamp first, then asserts the
// repair attempt receives the exact schema failure for that field.
type failingThenRepairGateway struct {
	calls    int
	repairPS string
}

func (g *failingThenRepairGateway) NativeToolCall(_ context.Context, _ string, systemPrompt string, _ any, _ []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.calls++
	if g.calls == 1 {
		return gateway.ToolCall{Name: "emit_bank_transaction", Arguments: []byte(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"DEBIT_CARD","amount_idr":"13120","transaction_at":"2026-08-28 10:00","merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.9}`)}, gateway.Metadata{}, nil
	}
	g.repairPS = systemPrompt
	return gateway.ToolCall{Name: "emit_bank_transaction", Arguments: []byte(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"DEBIT_CARD","amount_idr":"13120","transaction_at":"2026-08-28T10:00:00+07:00","merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.9}`)}, gateway.Metadata{}, nil
}

// A malformed transaction time must be repaired by the model, not silently
// converted, and the repair attempt must receive the precise failure.
func TestBankTimeRepairReceivesExactSchemaFailure(t *testing.T) {
	g := &failingThenRepairGateway{}
	got, _, err := NewExtractor(g).Extract(context.Background(), "src", Listener{BankName: "Bank"}, TrustedEmail{Body: "notice"})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if g.calls != 2 {
		t.Fatalf("attempts=%d, want 2", g.calls)
	}
	if got.TransactionAt == nil {
		t.Fatal("a repaired valid timestamp must be accepted")
	}
	if !strings.Contains(g.repairPS, "invalid transaction time") {
		t.Fatalf("repair prompt must carry the exact validation failure, got %q", g.repairPS)
	}
}

// A timezone-less or unparseable value is still canonical-invalid: it does not
// silently become received-at.
func TestTimezoneLessTimestampIsNotSilentlyAccepted(t *testing.T) {
	for _, raw := range []string{"2026-08-28 10:00", "2026-08-28T10:00:00", "garbage"} {
		if _, err := parseStrictBankRFC3339(raw); err == nil {
			t.Fatalf("%q must remain invalid", raw)
		}
	}
}
