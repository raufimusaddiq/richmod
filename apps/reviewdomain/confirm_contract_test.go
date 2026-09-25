package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// The transaction confirm operation must stay channel-neutral: both surfaces
// call ConfirmTransactionReview, and neither re-owns the transaction mutation.
func TestConfirmIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/handler.go",
		"../worker/internal/telegram/review.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), "reviewdomain.ConfirmTransactionReview") {
			t.Fatalf("%s does not call the shared confirm operation", path)
		}
		if strings.Contains(string(source), "UPDATE transaction SET status='CONFIRMED'") {
			t.Fatalf("%s still owns transaction confirm SQL", path)
		}
	}
}

// Telegram defers terminal completion while it asks about merchant learning, so
// the shared operation must support a non-resolving confirm.
func TestConfirmSupportsPartialResolution(t *testing.T) {
	if !strings.Contains(string(mustRead(t, "./confirm.go")), "if !cmd.ResolveReview {") {
		t.Fatal("shared confirm lost its partial-resolution path")
	}
	telegram, err := os.ReadFile("../worker/internal/telegram/review.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(telegram), "reviewID, userID, \"TELEGRAM_MERCHANT_DECISION\"") {
		t.Fatal("merchant-learning reply no longer completes the already-confirmed transaction review")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
