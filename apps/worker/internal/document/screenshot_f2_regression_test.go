package document

import (
	"context"
	"testing"
	"time"
)

func TestScreenshotF2DisputedPairMustNotAutoLink(t *testing.T) {
	t.Parallel()
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
