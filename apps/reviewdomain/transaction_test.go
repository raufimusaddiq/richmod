package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// Prevent either surface from replacing the shared household and stale-subject
// checks with adapter-local candidate validation.
func TestTransactionReviewAdaptersUseSharedValidation(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/handler.go",
		"../worker/internal/telegram/review.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), "reviewdomain.ConfirmTransactionReview") {
			t.Fatalf("%s bypasses shared transaction confirm", path)
		}
	}
}
