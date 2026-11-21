package reviewdomain

import (
	"strings"
	"testing"
)

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
