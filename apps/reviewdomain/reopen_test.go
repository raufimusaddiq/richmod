package reviewdomain

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// A reopened review asks only what the transaction still lacks; a supplied
// date, category or classification is never asked again.
func TestReopenedResidualsComeFromCanonicalFacts(t *testing.T) {
	for _, tt := range []struct {
		kind               string
		category, fallback bool
		want               []string
	}{
		{"EXPENSE", true, false, nil},
		{"EXPENSE", false, false, []string{"category"}},
		{"EXPENSE", true, true, []string{"transaction_at"}},
		{"INCOME", false, false, nil},
		{"UNCLASSIFIED", false, false, []string{"transfer_relationship"}},
	} {
		if got := reopenedResiduals(tt.kind, tt.category, tt.fallback); !slices.Equal(got, tt.want) {
			t.Errorf("%s category=%v fallback=%v: got %v want %v", tt.kind, tt.category, tt.fallback, got, tt.want)
		}
	}
}

// Only a vanished binding reads as already resolved; a transient failure must
// surface so the callback retries instead of closing the card.
func TestNotFoundAsResolvedKeepsTransientErrors(t *testing.T) {
	if !errors.Is(notFoundAsResolved(pgx.ErrNoRows), ErrAlreadyResolved) {
		t.Fatal("no rows must read as already resolved")
	}
	transient := errors.New("connection reset")
	if got := notFoundAsResolved(transient); !errors.Is(got, transient) || errors.Is(got, ErrAlreadyResolved) {
		t.Fatalf("transient error mapped to %v", got)
	}
}

func TestFormatRupiahRoundsHalfUp(t *testing.T) {
	for in, want := range map[string]string{"5000000.90": "5.000.001", "999.5": "1.000", "1250000.49": "1.250.000", "42000": "42.000"} {
		if got := FormatRupiah(in); got != want {
			t.Errorf("FormatRupiah(%s)=%s want %s", in, got, want)
		}
	}
}
