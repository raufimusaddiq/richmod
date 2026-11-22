package reviewdomain

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestBankFactValuesRejectUnqueueableFacts(t *testing.T) {
	for _, amount := range []string{"-54000", "+54000", "0", "000", "54,000", "999999999999999999999"} {
		if ValidateBankFactValues(amount, "2026-09-23T13:45:00+07:00") == nil {
			t.Fatalf("invalid amount %q accepted", amount)
		}
	}
	if err := ValidateBankFactValues("54000", "2026-09-23T13:45:00+07:00"); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeAliasFoldsUnicodeCompatibility(t *testing.T) {
	ascii := normalizeAlias("Bank BCA")
	if got := normalizeAlias("Ｂａｎｋ　ＢＣＡ"); got != ascii {
		t.Fatalf("full-width alias folded to %q, want %q", got, ascii)
	}
	if got := normalizeAlias("  Bank	BCA  "); got != ascii {
		t.Fatalf("whitespace alias folded to %q, want %q", got, ascii)
	}
	if normalizeAlias("") != "" {
		t.Fatal("empty alias must stay empty")
	}
}

func TestConfirmWithoutResolveReviewKeepsTheReviewOpen(t *testing.T) {
	t.Parallel()
	f := newLifecycleFixture(t)
	category := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan','food') RETURNING id`, f.household)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',7500,'IDR',now()) RETURNING id`, f.household)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN','{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	f.inTx(func(tx pgx.Tx) error {
		_, err := ConfirmTransactionReview(f.ctx, tx, ConfirmCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: transaction, CategorySupplied: true, CategoryID: category})
		return err
	})
	var transactionStatus, itemStatus string
	f.must(f.pool.QueryRow(f.ctx, `SELECT t.status,ri.status FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE ri.id=$1`, item).Scan(&transactionStatus, &itemStatus))
	if transactionStatus != "CONFIRMED" || itemStatus != "OPEN" {
		t.Fatalf("partial confirm left transaction=%s review=%s, want CONFIRMED with the review still OPEN", transactionStatus, itemStatus)
	}
	f.inTx(func(tx pgx.Tx) error {
		return ResolveByID(f.ctx, tx, Command{HouseholdID: f.household, ActorUserID: f.user, ReviewItemID: item, SubjectID: transaction, Action: "CONFIRM_REVIEW"})
	})
	f.must(f.pool.QueryRow(f.ctx, `SELECT status FROM review_item WHERE id=$1`, item).Scan(&itemStatus))
	if itemStatus != "RESOLVED" {
		t.Fatalf("resolving the item afterwards left review %s, want RESOLVED", itemStatus)
	}
}
