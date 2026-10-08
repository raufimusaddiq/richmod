package reviewdomain

import (
	"strings"
	"testing"
)

// Both surfaces must reject a transaction review through the same operation; no
// adapter may keep its own void/cancel SQL.
func TestRejectIsSharedAcrossSurfaces(t *testing.T) {
	for _, path := range []string{
		"../api/internal/review/handler.go",
		telegramReviewSourceGlob,
	} {
		text := string(mustReadPaths(t, path))
		start, end := "func (h *Handler) Reject(", "type mergeInput"
		if strings.Contains(path, "worker") {
			start, end = "func (p *Processor) rejectBoundReview(", "\nfunc "
		}
		from := strings.Index(text, start)
		to := strings.Index(text[from+len(start):], end)
		if from < 0 || to < 0 {
			t.Fatalf("%s reject handler not found", path)
		}
		text = text[from : from+to]
		if !strings.Contains(text, "reviewdomain.RejectTransactionReview") {
			t.Fatalf("%s does not call the shared reject operation", path)
		}
		if strings.Contains(text, "SET status='CANCELLED'") || strings.Contains(text, "proposal_status='REJECTED'") {
			t.Fatalf("%s still owns reject mutation SQL", path)
		}
	}
}
