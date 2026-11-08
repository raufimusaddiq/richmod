package document

import (
	"context"
	"testing"
)

// A payslip/receipt/screenshot job that runs out of attempts leaves the document
// CLASSIFIED and the source PROCESSED (classification settled them early). The
// final-failure step must fail both and leave the household a Tindakan item;
// running it twice, or after the document finished, changes nothing.
func TestTerminalDownstreamFailureFailsDocumentAndSource(t *testing.T) {
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
