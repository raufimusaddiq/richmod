package reviewdomain

import (
	"slices"
	"testing"
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
