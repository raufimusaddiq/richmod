package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// linkBankEvidence writes inside the transaction its caller opened and never
// commits it: the caller owns commit and rollback. A caller that rolls back
// therefore leaves no link, proposal or audit row behind.
func TestLinkBankEvidenceLeavesCommitToItsCaller(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
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
	at := time.Date(2026, 10, 19, 6, 30, 0, 0, time.UTC)
	var household, owner, account, source, existing string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Link ownership %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("link-owner-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	listener := Listener{HouseholdID: household, AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listener.ID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR',$2,now()) RETURNING id`, household, at).Scan(&existing))

	amount := "54000"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, TransactionAt: &at, Confidence: .95}
	tx, err := pool.Begin(ctx)
	must(err)
	defer tx.Rollback(ctx)
	must((&Processor{pool: pool}).linkBankEvidence(ctx, tx, listener, source, extraction, reviewdomain.LedgerMatch{ID: existing, Score: .95}, bankLink{household: household, amount: amount, at: at, description: "Belanja"}))
	// The caller's transaction is still open: linkBankEvidence did not end it.
	var linked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction_evidence WHERE transaction_id=$1 AND source_event_id=$2)`, existing, source).Scan(&linked); err != nil {
		t.Fatalf("linkBankEvidence ended its caller's transaction: %v", err)
	}
	if !linked {
		t.Fatal("the link is visible inside the caller's transaction")
	}
	must(tx.Rollback(ctx))
	var evidence, proposals, audits int
	must(pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction_evidence WHERE source_event_id=$1),(SELECT count(*) FROM transaction_proposal WHERE source_event_id=$1),(SELECT count(*) FROM audit_log WHERE household_id=$2 AND action='LINK_BANK_EMAIL_EVIDENCE')`, source, household).Scan(&evidence, &proposals, &audits))
	if evidence != 0 || proposals != 0 || audits != 0 {
		t.Fatalf("a caller rollback must discard the link: evidence=%d proposals=%d audits=%d", evidence, proposals, audits)
	}
}
