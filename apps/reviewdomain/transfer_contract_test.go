package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// Every surface must classify a transfer through the shared operation; no adapter
// may keep its own transaction/proposal mutation SQL.
func TestTransferIsSharedAcrossSurfaces(t *testing.T) {
	cases := []struct{ path, start, end string }{
		{"../api/internal/review/handler.go", "func (h *Handler) ClassifyTransfer(", "\nfunc "},
		{"../worker/internal/telegram/review.go", "func (p *Processor) resolveTransferReviewTx(", "\nfunc "},
		{"../worker/internal/telegram/agent_review_mutations.go", "func (p *Processor) agentResolveTransferClassification(", "\nfunc "},
	}
	for _, c := range cases {
		source, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		from := strings.Index(text, c.start)
		if from < 0 {
			t.Fatalf("%s transfer handler not found", c.path)
		}
		to := strings.Index(text[from+len(c.start):], c.end)
		if to < 0 {
			t.Fatalf("%s transfer handler end not found", c.path)
		}
		body := text[from : from+len(c.start)+to]
		if !strings.Contains(body, "reviewdomain.ClassifyTransferReview") {
			t.Fatalf("%s does not call the shared transfer operation", c.path)
		}
		if strings.Contains(body, "UPDATE transaction SET type=") || strings.Contains(body, "proposal_status=") {
			t.Fatalf("%s still owns transfer mutation SQL", c.path)
		}
	}
}
