package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PRD §37: Telegram and the Inbox must read one decision contract. A transaction
// that the source did not name a merchant for used to be reported to the Inbox as
// needing `merchant` while its stored decision asked only for `category`. The
// Inbox must serve the stored contract.
func TestTransactionBackedReviewExposesStoredDecision(t *testing.T) {
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

	stamp := time.Now().UnixNano()
	var household, user, source, transactionID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("inbox decision %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("inbox-decision-%d@example.test", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("inbox-decision-%d", stamp), []byte(fmt.Sprintf("inbox-decision-%d", stamp))).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,now()) RETURNING id`, household).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'BANK_EMAIL',1)`, transactionID, source); err != nil {
		t.Fatal(err)
	}
	decision, _ := json.Marshal(map[string]any{
		"version":           1,
		"reasonCode":        "UNKNOWN_MERCHANT",
		"decisionClass":     "EVIDENCE_GAP",
		"knownFacts":        map[string]any{"amount_idr": "54000"},
		"missingFacts":      []string{"category"},
		"whyNotAutoConfirm": "no decisive category",
		"allowedActions":    []string{"CONFIRM_REVIEW", "IGNORE"},
		"interactionMode":   "SINGLE_FIELD",
	})
	if _, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN',$3)`, household, transactionID, decision); err != nil {
		t.Fatal(err)
	}

	items := listCanonicalReviews(t, pool, household, user)
	if len(items) != 1 {
		t.Fatalf("expected the transaction-backed review, got %d", len(items))
	}
	if len(items[0].MissingFacts) != 1 || items[0].MissingFacts[0] != "category" {
		t.Fatalf("the Inbox must serve the stored decision's missing fact, got %v", items[0].MissingFacts)
	}
	if len(items[0].AllowedActions) != 2 || items[0].AllowedActions[0] != "CONFIRM_REVIEW" {
		t.Fatalf("the Inbox must serve the stored allowed actions, got %v", items[0].AllowedActions)
	}
	if items[0].WhyNotAuto == "" {
		t.Fatal("why-not-auto-confirm must reach the client")
	}
}
