package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestReviewIncompleteExtractionRecordsTheRealReason(t *testing.T) {
	t.Parallel()
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
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank review reason %d", testsupport.UniqueStamp())).Scan(&householdID); err != nil {
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
			if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("bank-reason-%d-%s", testsupport.UniqueStamp(), testCase.reviewType), []byte(testCase.name)).Scan(&sourceEventID); err != nil {
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

func TestBankSchemaFailureDoesNotCreateHumanReview(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var household, user, listener, source string
	stamp := testsupport.UniqueStamp()
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Schema failure %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("schema-%d@test.invalid", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Synthetic Bank',$2,$3) RETURNING id`, household, fmt.Sprintf("schema-%d@test.invalid", stamp), user).Scan(&listener); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprintf("schema-%d", stamp), []byte("schema")).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := (&Processor{pool: pool}).persistExtractionFailure(ctx, source, listener, "stub", "INVALID", "REPAIR"); err != nil {
		t.Fatal(err)
	}
	var status, validation, policy string
	var reviews int
	if err := pool.QueryRow(ctx, `SELECT s.processing_status,b.validation_status,b.policy_result,(SELECT count(*) FROM review_item WHERE source_event_id=s.id) FROM source_event s JOIN bank_email_extraction b ON b.source_event_id=s.id WHERE s.id=$1`, source).Scan(&status, &validation, &policy, &reviews); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED" || validation != "INVALID" || policy != "REPAIR" || reviews != 0 {
		t.Fatalf("schema failure must stay machine state: %s/%s/%s reviews=%d", status, validation, policy, reviews)
	}
}
