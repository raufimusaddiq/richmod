package document

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestTerminalDownstreamFailureFailsDocumentAndSource(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Terminal downstream failure")
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE document SET status='CLASSIFIED' WHERE id=$1`, f.documentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, f.sourceID); err != nil {
		t.Fatal(err)
	}
	run := func() {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := (&Processor{pool: f.pool}).TerminalDownstreamFailureTx(ctx, tx, f.documentID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run()
	run()
	var document, source string
	var actions int
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM integration_action WHERE household_id=d.household_id AND dedupe_key=s.id::text) FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source, &actions); err != nil {
		t.Fatal(err)
	}
	if document != "FAILED" || source != "FAILED" || actions != 1 {
		t.Fatalf("document=%s source=%s actions=%d, want FAILED/FAILED/1", document, source, actions)
	}
}

func TestTerminalDownstreamFailureKeepsAFinishedDocument(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Terminal downstream finished")
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE document SET status='EXTRACTED' WHERE id=$1`, f.documentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, f.sourceID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := (&Processor{pool: f.pool}).TerminalDownstreamFailureTx(ctx, tx, f.documentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var document, source string
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source); err != nil {
		t.Fatal(err)
	}
	if document != "EXTRACTED" || source != "PROCESSED" {
		t.Fatalf("document=%s source=%s changed by a terminal step on a finished document", document, source)
	}
}

func TestTerminalDownstreamFailureIgnoresResolvedReviewHistory(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Terminal downstream resolved history")
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE document SET status='CLASSIFIED' WHERE id=$1`, f.documentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, f.sourceID); err != nil {
		t.Fatal(err)
	}
	preset, ok := reviewdec.Preset("DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "document", f.documentID)
	if !ok {
		t.Fatal("missing preset")
	}
	encoded, err := preset.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,document_id,review_type,status,resolved_at,resolution_action,decision) VALUES($1,$2,$3,'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','RESOLVED',now(),'REPROCESS_DOCUMENT',$4::jsonb)`, f.householdID, f.sourceID, f.documentID, string(encoded)); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := (&Processor{pool: f.pool}).TerminalDownstreamFailureTx(ctx, tx, f.documentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, f.sourceID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "FAILED" {
		t.Fatalf("a reprocessed failure was hidden by historical resolved reviews: source=%s", source)
	}
}
