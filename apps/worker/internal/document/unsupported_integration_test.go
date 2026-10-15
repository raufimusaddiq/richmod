package document

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAuditF11UnsupportedFinancialEvidenceDoesNotPromiseReview(t *testing.T) {
	f := seedScreenshotFixture(t, "Synthetic unsupported evidence")
	ctx := context.Background()
	storage, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref := "synthetic/" + f.documentID + "/document.jpg"
	if err := storage.Put(ctx, ref, []byte("synthetic document"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE attachment SET storage_ref=$2 WHERE id=(SELECT attachment_id FROM document WHERE id=$1)`, f.documentID, ref); err != nil {
		t.Fatal(err)
	}
	gw := &classificationGateway{call: gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(`{"document_type":"OTHER_FINANCIAL_DOCUMENT","confidence":1,"reason":"no supported extraction"}`)}}
	if err := (&Processor{pool: f.pool, storage: storage, gateway: gw}).Process(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	var document, source string
	var reviews int
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM review_item WHERE household_id=d.household_id) FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source, &reviews); err != nil {
		t.Fatal(err)
	}
	if document != "CLASSIFIED" || source != "PROCESSED" || reviews != 0 {
		t.Fatalf("document=%s source=%s reviews=%d; unsupported evidence must be retained without unactionable review", document, source, reviews)
	}
}
