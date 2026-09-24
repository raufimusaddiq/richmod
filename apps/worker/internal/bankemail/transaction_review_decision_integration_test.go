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

// A transaction-backed bank review must reach the PRD §7 contract on the row the
// Inbox reads, not just return it from a helper (Hermes #126 round 3). The
// decision is written after EnqueueReviewRequest creates the review_item, so this
// pins the ordering that a silent zero-row UPDATE previously broke.
func TestTransactionReviewDecisionIsPersistedOnTheReviewRow(t *testing.T) {
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
	var householdID, userID, accountID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("bank decision %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Test','x') RETURNING id`, fmt.Sprintf("bank-decision-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, householdID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	var listenerID string
	if err = pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank','notifikasi@bank.test',$2,$3) RETURNING id`, householdID, accountID, userID).Scan(&listenerID); err != nil {
		t.Fatal(err)
	}
	var sourceEventID string
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("bank-decision-%d", stamp), []byte("decision")).Scan(&sourceEventID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(household_id,user_id,telegram_user_id) VALUES($1,$2,$3)`, householdID, userID, stamp); err != nil {
		t.Fatal(err)
	}

	amount, direction, channel, merchant := "54000", "OUTGOING", "DEBIT_CARD", "Warung Baru"
	at := time.Now().Add(-time.Hour).UTC()
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &merchant, TransactionAt: &at, Confidence: 0.9}
	listener := Listener{ID: listenerID, HouseholdID: householdID, BankName: "Bank", SenderAddress: "notifikasi@bank.test", AccountID: accountID, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := EvaluateBankEmail(listener, extraction, nil)
	if result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("fixture must park a transaction category review: %+v", result)
	}
	if err = (&Processor{pool: pool}).persist(ctx, listener, sourceEventID, extraction, result); err != nil {
		t.Fatal(err)
	}

	var transactionID string
	if err = pool.QueryRow(ctx, `SELECT transaction_id FROM transaction_evidence WHERE source_event_id=$1`, sourceEventID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT decision FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND status IN ('PENDING_SEND','OPEN')`, householdID, transactionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var decision reviewdec.Decision
	if err = json.Unmarshal(raw, &decision); err != nil {
		t.Fatal(err)
	}
	if decision.ReasonCode != "AMBIGUOUS_CATEGORY" || len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		t.Fatalf("transaction review decision not attached to the review row: %+v", decision)
	}
	if decision.Subject.ID != transactionID {
		t.Fatalf("decision subject=%q; want transaction %q", decision.Subject.ID, transactionID)
	}
}
