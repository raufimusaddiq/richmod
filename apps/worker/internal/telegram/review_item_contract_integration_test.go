package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// A NEEDS_REVIEW transaction in a household without Telegram still gets its
// canonical review_item, written with its ReviewDecision in the same INSERT.
// The item is idempotent per transaction and an incomplete decision is refused
// before it reaches the database.
func TestCreateTransactionReviewItemWritesCompleteContractOnce(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	var householdID, transactionID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("review contract %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,now()) RETURNING id`, householdID).Scan(&transactionID))

	create := func(status string, decisions ...reviewdec.Decision) (string, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return "", err
		}
		defer tx.Rollback(ctx)
		id, err := CreateTransactionReviewItem(ctx, tx, transactionID, "AMBIGUOUS_CATEGORY", status, decisions...)
		if err != nil {
			return "", err
		}
		return id, tx.Commit(ctx)
	}

	if _, err := create("OPEN", reviewdec.Decision{ReasonCode: "AMBIGUOUS_CATEGORY"}); err == nil {
		t.Fatal("a decision without allowedActions must be refused")
	}
	itemID, err := create("OPEN")
	must(err)
	var status, reasonCode string
	var actions int
	must(pool.QueryRow(ctx, `SELECT status,decision->>'reasonCode',jsonb_array_length(decision->'allowedActions') FROM review_item WHERE id=$1`, itemID).Scan(&status, &reasonCode, &actions))
	if status != "OPEN" || reasonCode != "AMBIGUOUS_CATEGORY" || actions == 0 {
		t.Fatalf("item status=%s reasonCode=%s actions=%d; want OPEN with a complete preset decision", status, reasonCode, actions)
	}
	var requests int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE review_item_id=$1`, itemID).Scan(&requests))
	if requests != 0 {
		t.Fatalf("a no-Telegram item must not create a projection: requests=%d", requests)
	}

	override, ok := reviewdec.Preset("UNKNOWN_MERCHANT", "transaction", transactionID)
	if !ok {
		t.Fatal("no UNKNOWN_MERCHANT preset")
	}
	again, err := create("PENDING_SEND", override)
	must(err)
	if again != itemID {
		t.Fatalf("second create returned %s; want the active item %s", again, itemID)
	}
	must(pool.QueryRow(ctx, `SELECT status,decision->>'reasonCode' FROM review_item WHERE id=$1`, itemID).Scan(&status, &reasonCode))
	if status != "OPEN" || reasonCode != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("idempotent create overwrote the active item: status=%s reasonCode=%s", status, reasonCode)
	}
	var active int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE transaction_id=$1 AND status IN ('OPEN','PENDING_SEND')`, transactionID).Scan(&active))
	if active != 1 {
		t.Fatalf("active items=%d; want exactly one", active)
	}
}

// The 00078 trigger is the database guard behind reviewdec.Validate: no writer
// may insert an item without a complete decision or degrade a stored one, while
// a historical NULL-decision row stays updatable.
func TestReviewItemDecisionTriggerGuardsNewRowsOnly(t *testing.T) {
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
	var householdID, sourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("review trigger %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("review-trigger-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	insert := func(decision any) (string, error) {
		var id string
		err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),$3::jsonb) RETURNING id`, householdID, sourceID, decision).Scan(&id)
		return id, err
	}
	for name, decision := range map[string]any{
		"null":              nil,
		"empty":             `{}`,
		"blank reason":      `{"reasonCode":" ","allowedActions":["IGNORE"]}`,
		"no actions":        `{"reasonCode":"AMBIGUOUS_CATEGORY"}`,
		"empty actions":     `{"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":[]}`,
		"actions not array": `{"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":"IGNORE"}`,
	} {
		if _, err := insert(decision); err == nil {
			t.Fatalf("%s: trigger accepted an incomplete decision", name)
		}
	}
	valid, err := insert(`{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE review_item SET decision=NULL WHERE id=$1`, valid); err == nil {
		t.Fatal("trigger allowed erasing a stored decision")
	}
	if _, err := pool.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{missingFacts}','["category"]'::jsonb) WHERE id=$1`, valid); err != nil {
		t.Fatalf("a complete decision must stay editable: %v", err)
	}

	legacy := ""
	execWithoutReviewDecisionTrigger(t, pool, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now())`, householdID, sourceID)
	if err := pool.QueryRow(ctx, `SELECT id FROM review_item WHERE source_event_id=$1 AND decision IS NULL`, sourceID).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE review_item SET resolution_action='IGNORE',decision=NULL WHERE id=$1`, legacy); err != nil {
		t.Fatalf("a legacy NULL-decision row must stay updatable untouched: %v", err)
	}
	// Reopening a legacy row would put a contract-less review back in the Inbox
	// and Telegram, which render only stored contracts.
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='OPEN',resolved_at=NULL WHERE id=$1`, legacy); err == nil {
		t.Fatal("trigger allowed a legacy NULL-decision row to become active")
	}
}
