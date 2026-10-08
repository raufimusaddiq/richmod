package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// COMPLETE_BANK_REVIEW is the job the Web and Telegram lanes queue once a
// household supplies the amount and time an unverified bank email lacked. The
// worker runs Complete; this drives that entry with the stored extraction.
func TestCompleteBankReviewRecordsSuppliedFactsOnce(t *testing.T) {
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
	var householdID, userID, accountID, listenerID, sourceEventID, itemID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank complete %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-complete-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, householdID).Scan(&accountID))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank','notifikasi@bank.test',$2,$3) RETURNING id`, householdID, accountID, userID).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("bank-complete-%d", stamp), []byte("bank")).Scan(&sourceEventID))
	extraction := `{"kind":"TRANSACTION","direction":"OUT","channel":"CARD","amount_idr":null,"transaction_at":null,"merchant":"Indomaret","counterparty":null,"reference":null,"description":null,"missing_fields":["amount_idr","transaction_at"],"confidence":0.9}`
	_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'IMAP','v1',$3::jsonb,'INVALID')`, sourceEventID, listenerID, extraction)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN','{}'::jsonb) RETURNING id`, householdID, sourceEventID).Scan(&itemID))

	amount, at := "54000", "2026-09-23T13:45:00+07:00"
	payload := Payload{SourceEventID: sourceEventID, ReviewID: itemID, AmountIDR: &amount, TransactionAt: &at}
	processor := &Processor{pool: pool}
	must(processor.Complete(ctx, payload))

	var itemStatus, validation string
	must(pool.QueryRow(ctx, `SELECT ri.status,e.validation_status FROM review_item ri JOIN bank_email_extraction e ON e.source_event_id=ri.source_event_id WHERE ri.id=$1`, itemID).Scan(&itemStatus, &validation))
	if itemStatus != "RESOLVED" || validation != "VALID" {
		t.Fatalf("review=%s extraction=%s, want RESOLVED/VALID", itemStatus, validation)
	}
	var proposals int
	var proposedAmount string
	must(pool.QueryRow(ctx, `SELECT count(*),COALESCE(max(amount)::text,'') FROM transaction_proposal WHERE source_event_id=$1`, sourceEventID).Scan(&proposals, &proposedAmount))
	if proposals != 1 || proposedAmount != "54000" && proposedAmount != "54000.00" {
		t.Fatalf("proposals=%d amount=%s, want one proposal for 54000", proposals, proposedAmount)
	}

	// A replayed job is a no-op: the review is resolved and nothing is recorded twice.
	must(processor.Complete(ctx, payload))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction_proposal WHERE source_event_id=$1`, sourceEventID).Scan(&proposals))
	if proposals != 1 {
		t.Fatalf("replay recorded the bank email again: proposals=%d", proposals)
	}
}
