package reviewdomain

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Ignoring a document review must satisfy document_status_check (which has no
// IGNORED value): the source event ends IGNORED and the document settles.
func TestResolveDocumentReviewIgnoreSettlesDocumentAndSource(t *testing.T) {
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
	f := seedMergedScreenshot(t, ctx, pool, "ignore-document")
	// A low-confidence document has produced no accepted work; the seed's merged
	// proposal and evidence would (correctly) settle the source as PROCESSED.
	_, err = pool.Exec(ctx, `DELETE FROM transaction_evidence WHERE id=$1`, f.evidence)
	must(err)
	_, err = pool.Exec(ctx, `DELETE FROM transaction_proposal WHERE id=$1`, f.proposal)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE document SET status='NEEDS_REVIEW' WHERE id=$1`, f.document)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW' WHERE id=$1`, f.source)
	must(err)
	var item string
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,document_id,source_event_id,review_type,status,decision)
		VALUES($1,$2,$3,'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','OPEN',$4::jsonb) RETURNING id::text`,
		f.household, f.document, f.source, `{"version":1,"reasonCode":"LOW_CONFIDENCE_EXTRACTION","allowedActions":["REPROCESS_DOCUMENT","IGNORE"]}`).Scan(&item))

	tx, err := pool.Begin(ctx)
	must(err)
	defer tx.Rollback(ctx)
	result, err := ResolveDocumentReview(ctx, tx, DocumentReviewCommand{HouseholdID: f.household, UserID: f.user, ReviewItemID: item, DocumentID: f.document, ActorType: "USER", Action: "IGNORE"})
	if err != nil {
		t.Fatalf("ignore failed: %v", err)
	}
	must(tx.Commit(ctx))
	if result.SourceEventID != f.source {
		t.Fatalf("source event %s; want %s", result.SourceEventID, f.source)
	}

	var itemStatus, resolution, documentStatus, sourceStatus string
	must(pool.QueryRow(ctx, `SELECT ri.status,ri.resolution_action,d.status,s.processing_status FROM review_item ri
		JOIN document d ON d.id=ri.document_id JOIN source_event s ON s.id=d.source_event_id WHERE ri.id=$1`, item).Scan(&itemStatus, &resolution, &documentStatus, &sourceStatus))
	if itemStatus != "RESOLVED" || resolution != "IGNORE" || documentStatus != "EXTRACTED" || sourceStatus != "IGNORED" {
		t.Fatalf("item=%s/%s document=%s source=%s; want RESOLVED/IGNORE EXTRACTED IGNORED", itemStatus, resolution, documentStatus, sourceStatus)
	}
}
