package reviewdomain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A reprocessed screenshot names the transaction it was already linked to in
// the review decision; merging into it must work even after amount and date
// drifted out of the usual duplicate rules, and only for that carried target.
func TestMergeDuplicateAcceptsCarriedPriorMatchOutsideAmountAndWindow(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var household, user, source, carried, bystander, foreign, otherHousehold, item string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("prior merge %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("prior merge other %d", stamp)).Scan(&otherHousehold))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("prior-merge-%d@example.test", stamp)).Scan(&user))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user)
	must(err)
	insert := func(h, status string, amount int, ago string) string {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,confirmed_at) VALUES($1,'EXPENSE',$2,$3,'IDR',now()-$4::interval,'Indomaret',CASE WHEN $2='CONFIRMED' THEN now() END) RETURNING id`, h, status, amount, ago).Scan(&id))
		return id
	}
	source = insert(household, "NEEDS_REVIEW", 57500, "1 hour")
	carried = insert(household, "CONFIRMED", 58000, "200 hours")
	bystander = insert(household, "CONFIRMED", 59000, "210 hours")
	foreign = insert(otherHousehold, "CONFIRMED", 58000, "200 hours")
	decision := fmt.Sprintf(`{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"],"knownFacts":{"prior_match_transaction_ids":["%s","%s"]}}`, carried, foreign)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb) RETURNING id`, household, source, decision).Scan(&item))

	merge := func(target string) error {
		tx, err := pool.Begin(ctx)
		must(err)
		defer tx.Rollback(ctx)
		if _, err := MergeDuplicateReview(ctx, tx, DuplicateCommand{HouseholdID: household, ActorUserID: user, TransactionID: source, TargetTransactionID: target, ReviewItemID: item}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for name, target := range map[string]string{"drifted but not carried": bystander, "carried id of another household": foreign} {
		if err := merge(target); !errors.Is(err, ErrDuplicateTargetInvalid) {
			t.Fatalf("%s must be refused: %v", name, err)
		}
	}
	must(merge(carried))
	var status string
	must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, source).Scan(&status))
	if status != "VOIDED" {
		t.Fatalf("source status after merge = %s", status)
	}
}
