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

// A verified-but-unsupported ruling must leave its audit row behind. That row is
// the only record of what the plane actually claimed for exactly the case the
// review path parks (every SAVR-06 bank residual), so persistence must run
// before the review branch returns (SAVR-06, Hermes round 4).
func TestBankEvidenceVerificationIsPersistedForAnUnsupportedRuling(t *testing.T) {
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
	var householdID, userID, listenerID, sourceEventID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank evidence audit %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-audit-%d@example.test", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Synthetic Bank',$2,$3) RETURNING id`, householdID, fmt.Sprintf("listener-%d@example.test", time.Now().UnixNano()), userID).Scan(&listenerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("bank-audit-%d", time.Now().UnixNano()), []byte("audit")).Scan(&sourceEventID); err != nil {
		t.Fatal(err)
	}
	verification := EvidenceVerification{
		PolicyVersion: BankEmailVerificationPolicyVersion,
		ClaimOutcomes: map[string]string{"transaction_observed": "YES", "amount_supported": "YES", "direction_supported": "YES", "semantic_grounded": "NO", "material_ambiguity": "NO"},
	}
	if err = (&Processor{pool: pool}).persistEvidenceVerification(ctx, sourceEventID, listenerID, "stub", verification); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT answer_summary_json FROM bank_email_evidence_verification WHERE source_event_id=$1`, sourceEventID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ClaimOutcomes map[string]string `json:"claim_outcomes"`
	}
	if err = json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ClaimOutcomes["semantic_grounded"] != "NO" {
		t.Fatalf("the audit row must keep the exact predicate outcome: %+v", summary.ClaimOutcomes)
	}
}
