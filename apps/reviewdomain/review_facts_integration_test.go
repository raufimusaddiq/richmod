package reviewdomain

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditF07TransferRequiresChosenDate(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, classification := range []string{"OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT"} {
		t.Run(classification, func(t *testing.T) {
			f := seedMergedScreenshot(t, ctx, pool, "chosen-transfer-date")
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := pool.Exec(ctx, `UPDATE transaction SET type='INCOME',status='NEEDS_REVIEW',confirmed_at=NULL WHERE id=$1`, f.original)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE transaction_proposal SET proposed_type='INCOME',proposal_status='NEEDS_REVIEW',metadata_json=metadata_json||'{"date_known":false}' WHERE id=$1`, f.proposal)
			must(err)
			tx, err := pool.Begin(ctx)
			must(err)
			_, err = ClassifyTransferReview(ctx, tx, TransferCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, Classification: classification})
			var missing *ErrMissingFacts
			if !errors.As(err, &missing) || len(missing.Facts) != 1 || missing.Facts[0] != "transaction_at" {
				t.Fatalf("fallback date accepted: %v", err)
			}
			must(tx.Rollback(ctx))
			at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.FixedZone("WIB", 7*3600))
			tx, err = pool.Begin(ctx)
			must(err)
			defer tx.Rollback(ctx)
			_, err = ClassifyTransferReview(ctx, tx, TransferCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, Classification: classification, TransactionAt: &at})
			must(err)
			must(tx.Commit(ctx))
			var status, kind, provenance string
			var got time.Time
			must(pool.QueryRow(ctx, `SELECT t.status,t.type,t.transaction_at,p.metadata_json->>'date_source' FROM transaction t,transaction_proposal p WHERE t.id=$1 AND p.id=$2`, f.original, f.proposal).Scan(&status, &kind, &got, &provenance))
			if status != "CONFIRMED" || kind != "TRANSFER" || !got.Equal(at) || provenance != "USER" {
				t.Fatalf("status=%s type=%s dateMatches=%v provenance=%s", status, kind, got.Equal(at), provenance)
			}
		})
	}
}
