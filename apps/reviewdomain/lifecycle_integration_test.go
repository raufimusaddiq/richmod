package reviewdomain

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditLifecycleSettlesAllOutstandingWork(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, scenario := range []string{"proposal_sibling", "document_complete", "document_sibling", "transfer_sibling", "album_member"} {
		t.Run(scenario, func(t *testing.T) {
			f := seedMergedScreenshot(t, ctx, pool, scenario)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := pool.Exec(ctx, `UPDATE transaction SET status='NEEDS_REVIEW',confirmed_at=NULL,category_id=$2 WHERE id=$1`, f.original, f.category)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='NEEDS_REVIEW' WHERE id=$1`, f.proposal)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE document SET status='NEEDS_REVIEW' WHERE id=$1`, f.document)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW' WHERE id=$1`, f.source)
			must(err)
			var secondary string
			if scenario == "proposal_sibling" {
				_, err = pool.Exec(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,'row-002','EXPENSE',NULL,now(),.9,'NEEDS_REVIEW',jsonb_build_object('document_id',$3::uuid))`, f.household, f.source, f.document)
				must(err)
			}
			if scenario == "document_sibling" || scenario == "transfer_sibling" {
				var sibling string
				must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',101,now()) RETURNING id`, f.household).Scan(&sibling))
				_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',jsonb_build_object('document_id',$3::uuid))`, sibling, f.source, f.document)
				must(err)
			}
			if scenario == "album_member" {
				must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',now(),'synthetic-secondary','PROCESSING') RETURNING id`, f.household).Scan(&secondary))
				_, err = pool.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) SELECT id,$2,attachment_id,1 FROM document WHERE id=$1`, f.document, secondary)
				must(err)
			}
			tx, err := pool.Begin(ctx)
			must(err)
			defer tx.Rollback(ctx)
			if scenario == "transfer_sibling" {
				_, err = tx.Exec(ctx, `UPDATE transaction SET type='INCOME',category_id=NULL WHERE id=$1`, f.original)
				must(err)
				_, err = ClassifyTransferReview(ctx, tx, TransferCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, Classification: "HOUSEHOLD_ACCOUNT"})
				must(err)
			} else {
				_, err = ConfirmTransactionReview(ctx, tx, ConfirmCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: f.original, ResolveReview: true})
				must(err)
				if scenario == "document_sibling" {
					must(PromoteEvidenceDocuments(ctx, tx, f.original))
				}
			}
			must(tx.Commit(ctx))
			var sourceStatus, documentStatus string
			must(pool.QueryRow(ctx, `SELECT s.processing_status,d.status FROM source_event s JOIN document d ON d.source_event_id=s.id WHERE s.id=$1`, f.source).Scan(&sourceStatus, &documentStatus))
			wantSource, wantDocument := "PROCESSED", "EXTRACTED"
			if scenario == "proposal_sibling" || scenario == "document_sibling" || scenario == "transfer_sibling" {
				wantSource, wantDocument = "NEEDS_REVIEW", "NEEDS_REVIEW"
			}
			if secondary != "" {
				must(pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, secondary).Scan(&sourceStatus))
			}
			if sourceStatus != wantSource || documentStatus != wantDocument {
				t.Fatalf("source=%s document=%s; want %s/%s", sourceStatus, documentStatus, wantSource, wantDocument)
			}
		})
	}
}
