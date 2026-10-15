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

func TestTransferPartialClassificationRetainsAndRevalidatesCandidates(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, choice := range []struct{ classification, kind, purpose, wealthRole string }{
		{"EXPENSE", "EXPENSE", "GENERAL", ""},
		{"INVESTMENT_ACCOUNT", "TRANSFER", "INVESTMENT_CONTRIBUTION", "INVESTMENT"},
		{"ASSET_PURCHASE", "TRANSFER", "ASSET_PURCHASE", "OTHER"},
	} {
		t.Run(choice.classification, func(t *testing.T) {
			f := seedMergedScreenshot(t, ctx, pool, "partial-transfer")
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := pool.Exec(ctx, `UPDATE transaction SET type='UNCLASSIFIED',status='NEEDS_REVIEW',confirmed_at=NULL WHERE id=$1`, f.original)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE transaction_proposal SET proposed_type='UNCLASSIFIED',proposal_status='NEEDS_REVIEW',metadata_json=metadata_json||'{"date_known":false}' WHERE id=$1`, f.proposal)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","missingFacts":["transfer_relationship","transaction_at"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}'::jsonb)`, f.household, f.original)
			must(err)
			var wealth, category string
			if choice.wealthRole != "" {
				must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic asset','ASSET','GOLD',$2) RETURNING id`, f.household, choice.wealthRole).Scan(&wealth))
			} else {
				category = f.category
			}
			tx, err := pool.Begin(ctx)
			must(err)
			defer tx.Rollback(ctx)
			partial, err := ClassifyTransferReview(ctx, tx, TransferCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, Classification: choice.classification, CategoryID: category, WealthAccountID: wealth})
			var missing *ErrMissingFacts
			if !errors.As(err, &missing) || len(missing.Facts) != 1 || missing.Facts[0] != "transaction_at" || partial.Status != "NEEDS_REVIEW" {
				t.Fatalf("partial status=%s err=%v", partial.Status, err)
			}
			must(tx.Commit(ctx))
			at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.FixedZone("WIB", 7*3600))
			if wealth != "" {
				_, err = pool.Exec(ctx, `UPDATE wealth_account SET active=false WHERE id=$1`, wealth)
			} else {
				_, err = pool.Exec(ctx, `UPDATE category SET active=false WHERE id=$1`, category)
			}
			must(err)
			tx, err = pool.Begin(ctx)
			must(err)
			_, err = ConfirmTransactionReview(ctx, tx, ConfirmCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, TransactionAt: &at, ResolveReview: true})
			wantErr := ErrInvalidCandidate
			if wealth != "" {
				wantErr = ErrWealthAccountIncompatible
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("inactive candidate: got %v; want %v", err, wantErr)
			}
			must(tx.Rollback(ctx))
			if wealth != "" {
				_, err = pool.Exec(ctx, `UPDATE wealth_account SET active=true WHERE id=$1`, wealth)
			} else {
				_, err = pool.Exec(ctx, `UPDATE category SET active=true WHERE id=$1`, category)
			}
			must(err)
			tx, err = pool.Begin(ctx)
			must(err)
			defer tx.Rollback(ctx)
			_, err = ConfirmTransactionReview(ctx, tx, ConfirmCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, TransactionAt: &at, ResolveReview: true})
			must(err)
			must(tx.Commit(ctx))
			var kind, status, purpose, gotWealth, gotCategory, proposalKind, proposalStatus, proposalChoice string
			must(pool.QueryRow(ctx, `SELECT t.type,t.status,t.purpose,COALESCE(t.related_wealth_account_id::text,''),COALESCE(t.category_id::text,''),p.proposed_type,p.proposal_status,p.metadata_json->>'transfer_classification' FROM transaction t,transaction_proposal p WHERE t.id=$1 AND p.id=$2`, f.original, f.proposal).Scan(&kind, &status, &purpose, &gotWealth, &gotCategory, &proposalKind, &proposalStatus, &proposalChoice))
			if kind != choice.kind || status != "CONFIRMED" || purpose != choice.purpose || gotWealth != wealth || gotCategory != category || proposalKind != choice.kind || proposalStatus != "ACCEPTED" || proposalChoice != choice.classification {
				t.Fatalf("transaction=%s/%s/%s wealthMatches=%v categoryMatches=%v proposal=%s/%s/%s", kind, status, purpose, gotWealth == wealth, gotCategory == category, proposalKind, proposalStatus, proposalChoice)
			}
		})
	}
}
