package document

import (
	"context"
	"testing"
	"time"
)

// F2 regression (disputed pair 926/928, synthetic values): same merchant, same
// amount, printed times 10 minutes apart, the first already a confirmed
// transaction backed by its own screenshot evidence. A distinct payment must
// never reach the strong auto-link threshold (>= .90). On the unmodified
// 66c1a9c matcher this fails with score 0.95 (see
// docs/audits/F2-screenshot-false-merge-repro.md for the old-code twin).
func TestScreenshotF2DisputedPairMustNotAutoLink(t *testing.T) {
	at := screenshotRowTime()
	for _, tc := range []struct {
		name        string
		priorFacts  string
		identifiers []screenshotIdentifier
	}{
		{"legacy evidence without identifiers", `{"date_known":true}`, nil},
		{"distinct printed identifiers", `{"transaction_identifiers":[{"kind":"TRANSACTION_ID","value":"SYNTHETIC-A"}],"date_known":true,"transaction_time_precision":"TIME"}`, []screenshotIdentifier{{"TRANSACTION_ID", "SYNTHETIC-B"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := seedScreenshotFixture(t, "Synthetic F2 pair "+tc.name)
			first := seedScreenshotIdentityCandidate(t, f, tc.priorFacts, at.Add(-10*time.Minute))
			row := validatedScreenshotRow{Value: screenshotRow{Amount: ptr("7500"), Merchant: "Synthetic Store", TransactionIdentifiers: tc.identifiers, TransactionTimePrecision: "TIME"}, Type: "EXPENSE", TransactionAt: at, DateKnown: true}
			matches, err := (&Processor{pool: f.pool}).findScreenshotMatches(context.Background(), f.householdID, row)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range matches {
				if candidate.ID == first && candidate.Score >= .90 {
					t.Fatalf("second distinct payment reaches the auto-link threshold (score %.2f)", candidate.Score)
				}
			}
		})
	}
}
