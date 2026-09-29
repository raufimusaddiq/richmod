package reviewdomain

import (
	"strings"
	"testing"
)

// Prevent either surface from replacing the shared household and stale-subject
// checks with adapter-local candidate validation.
func TestTransactionReviewAdaptersUseSharedValidation(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/handler.go",
		telegramReviewSourceGlob,
	} {
		if !strings.Contains(string(mustReadPaths(t, path)), "reviewdomain.ConfirmTransactionReview") {
			t.Fatalf("%s bypasses shared transaction confirm", path)
		}
	}
}
