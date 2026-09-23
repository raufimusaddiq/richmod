package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// reviewIncompleteExtraction used to hardcode UNKNOWN_BANK_TEMPLATE for every
// rejection reason, so a structurally incomplete email and a semantically
// unsupported one were indistinguishable in the Review Inbox. Pin the reason
// that actually fired.
func TestReviewIncompleteExtractionRecordsTheRealReason(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var householdID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank review reason %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}

	for _, testCase := range []struct {
		name       string
		reviewType string
	}{
		{"structural incompleteness", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE"},
		{"semantic rejection", "UNKNOWN_BANK_TEMPLATE"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var sourceEventID string
			if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("bank-reason-%d-%s", time.Now().UnixNano(), testCase.reviewType), []byte(testCase.name)).Scan(&sourceEventID); err != nil {
				t.Fatal(err)
			}
			if err := processor.reviewIncompleteExtraction(ctx, householdID, sourceEventID, ToolSchemaVersion, testCase.reviewType, partialDecision(householdID, sourceEventID, Extraction{}, testCase.reviewType, []string{"amount"}, "test")); err != nil {
				t.Fatal(err)
			}
			var reviewType, status string
			if err := pool.QueryRow(ctx, `SELECT review_type,status FROM review_item WHERE source_event_id=$1`, sourceEventID).Scan(&reviewType, &status); err != nil {
				t.Fatal(err)
			}
			if reviewType != testCase.reviewType {
				t.Fatalf("review_type=%q; want %q", reviewType, testCase.reviewType)
			}
			if status != "OPEN" {
				t.Fatalf("status=%q; want OPEN", status)
			}
			// The PRD §7 contract must be stored so the Inbox can explain the review
			// without re-deriving it. Decision class, reason, and a why-not-auto-confirm
			// reason are the minimum a client needs.
			var decision reviewdec.Decision
			var rawDecision []byte
			if err := pool.QueryRow(ctx, `SELECT decision FROM review_item WHERE source_event_id=$1`, sourceEventID).Scan(&rawDecision); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rawDecision, &decision); err != nil {
				t.Fatal(err)
			}
			if decision.ReasonCode != testCase.reviewType || decision.DecisionClass == "" || decision.WhyNotAuto == "" || len(decision.MissingFacts) == 0 {
				t.Fatalf("stored decision is incomplete: %+v", decision)
			}
			if decision.Subject.ID != sourceEventID {
				t.Fatalf("decision subject=%q; want source event %q", decision.Subject.ID, sourceEventID)
			}
		})
	}
}
