package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// identityGateway answers the extraction call with a fixed extraction and
// records every repair prompt. repair controls what a repair call returns.
type identityGateway struct {
	t             *testing.T
	extraction    screenshotExtraction
	repairPrompts []string
	extractions   int
}

func (g *identityGateway) NativeToolCall(_ context.Context, _ string, prompt string, _ any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	switch tools[0].Name {
	case "extract_transaction_screenshot":
		g.extractions++
		args, err := json.Marshal(g.extraction)
		return gateway.ToolCall{Name: "extract_transaction_screenshot", Arguments: args}, gateway.Metadata{Model: "synthetic"}, err
	case "repair_transaction_fields":
		g.repairPrompts = append(g.repairPrompts, prompt)
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("repair unavailable")
	}
	g.t.Fatalf("unexpected tool %s", tools[0].Name)
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func runIdentityScreenshot(t *testing.T, label string, extraction screenshotExtraction) (screenshotFixture, *identityGateway, *Processor) {
	t.Helper()
	f := seedScreenshotFixture(t, label)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE document SET document_type='EWALLET_SCREENSHOT' WHERE id=$1`, f.documentID); err != nil {
		t.Fatal(err)
	}
	storage, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	if err := f.pool.QueryRow(ctx, `SELECT a.storage_ref FROM document d JOIN attachment a ON a.id=d.attachment_id WHERE d.id=$1`, f.documentID).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(ctx, ref, []byte("synthetic screenshot page"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	gw := &identityGateway{t: t, extraction: extraction}
	p := &Processor{pool: f.pool, gateway: gw, storage: storage}
	if err := p.ProcessScreenshot(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	return f, gw, p
}

// The incident: two reference numbers must not fail a correct extraction.
func TestIncidentDuplicateReferenceNumbersStillRecordThePayment(t *testing.T) {
	at := time.Now().In(jakarta()).Add(-time.Hour).Format(time.RFC3339)
	f, gw, _ := runIdentityScreenshot(t, "Identity duplicate kinds", screenshotExtraction{Confidence: .99, Transactions: []screenshotRow{incidentRow(at)}})
	ctx := context.Background()
	if len(gw.repairPrompts) != 0 {
		t.Fatalf("a normalizable identity must not spend the repair pass: %v", gw.repairPrompts)
	}
	var document, source, amount string
	var proposals, reviews int
	var identifiers string
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM transaction_proposal WHERE source_event_id=s.id),(SELECT count(*) FROM review_item WHERE household_id=s.household_id AND source_event_id=s.id),
		COALESCE((SELECT amount::text FROM transaction_proposal WHERE source_event_id=s.id LIMIT 1),''),COALESCE((SELECT metadata_json->'transaction_identifiers'::text FROM transaction_proposal WHERE source_event_id=s.id LIMIT 1)::text,'')
		FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source, &proposals, &reviews, &amount, &identifiers); err != nil {
		t.Fatal(err)
	}
	if document == "FAILED" || source == "FAILED" || proposals != 1 || amount != "72001" {
		t.Fatalf("document=%s source=%s proposals=%d amount=%s", document, source, proposals, amount)
	}
	if strings.Count(identifiers, `"REFERENCE_NUMBER"`) != 1 || !strings.Contains(identifiers, "631703300775") || strings.Contains(identifiers, "1u4lajs36358") {
		t.Fatalf("stored identity is not the first identifier per kind: %s", identifiers)
	}
	var audits int
	var rows, values string
	if err := f.pool.QueryRow(ctx, `SELECT count(*),COALESCE(max((after_json->'rows')::text),''),COALESCE(max(after_json::text),'') FROM audit_log WHERE household_id=$1 AND action='NORMALIZE_SCREENSHOT_IDENTIFIERS' AND entity_id=$2 AND after_json->>'document_id'=$3`, f.householdID, f.sourceID, f.documentID).Scan(&audits, &rows, &values); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || !strings.Contains(rows, "REFERENCE_NUMBER") {
		t.Fatalf("normalization must be recorded once: audits=%d rows=%s", audits, rows)
	}
	if strings.Contains(values, "631703300775") || strings.Contains(values, "1u4lajs36358") {
		t.Fatalf("audit must not store identifier values: %s", values)
	}
}

// An identity that stays invalid after normalization and repair is not a dead
// end: the household gets a document-bound review, nothing is written to the
// ledger, and rerunning the job does not open a second review.
func TestUnrecoverableScreenshotIdentityOpensAReview(t *testing.T) {
	at := time.Now().In(jakarta()).Add(-time.Hour).Format(time.RFC3339)
	row := incidentRow(at)
	row.TransactionIdentifiers = []screenshotIdentifier{{"ACCOUNT_NUMBER", "123456"}, {"REFERENCE_NUMBER", "a"}, {"REFERENCE_NUMBER", "b"}}
	f, gw, p := runIdentityScreenshot(t, "Identity unrecoverable", screenshotExtraction{Confidence: .99, Transactions: []screenshotRow{row}})
	ctx := context.Background()
	if len(gw.repairPrompts) != 1 || !strings.Contains(gw.repairPrompts[0], "identifier kind must be") || !strings.Contains(gw.repairPrompts[0], "duplicate identifier kind REFERENCE_NUMBER") {
		t.Fatalf("the single repair pass must name the broken rules: %v", gw.repairPrompts)
	}
	check := func() (reviewID string) {
		var document, source string
		var proposals, transactions, items int
		var reason, kind string
		var actions []byte
		if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM transaction_proposal WHERE source_event_id=s.id),(SELECT count(*) FROM transaction WHERE household_id=s.household_id),
			(SELECT count(*) FROM review_item WHERE household_id=s.household_id AND document_id=d.id),
			(SELECT after_json->>'reason' FROM audit_log WHERE entity_id=s.id AND action='REJECT_DOCUMENT_EXTRACTION' ORDER BY created_at DESC LIMIT 1)
			FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source, &proposals, &transactions, &items, &reason); err != nil {
			t.Fatal(err)
		}
		if document != "FAILED" || source != "NEEDS_REVIEW" || proposals != 0 || transactions != 0 || items != 1 {
			t.Fatalf("document=%s source=%s proposals=%d transactions=%d reviews=%d", document, source, proposals, transactions, items)
		}
		if !strings.Contains(reason, "INVALID_TRANSACTION_IDENTITY") || !strings.Contains(reason, "REPAIR_FAILED") {
			t.Fatalf("audit reason lost the failing rule: %s", reason)
		}
		if err := f.pool.QueryRow(ctx, `SELECT id,review_type,status,decision->'allowedActions' FROM review_item WHERE household_id=$1 AND document_id=$2`, f.householdID, f.documentID).Scan(&reviewID, &kind, &document, &actions); err != nil {
			t.Fatal(err)
		}
		if kind != "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" || document != "OPEN" || !strings.Contains(string(actions), "REPROCESS_DOCUMENT") || !strings.Contains(string(actions), "IGNORE") {
			t.Fatalf("review kind=%s status=%s actions=%s", kind, document, actions)
		}
		return reviewID
	}
	reviewID := check()
	// A retried job re-runs the extraction and must reuse the open review.
	if err := p.ProcessScreenshot(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	if again := check(); again != reviewID {
		t.Fatalf("retry opened a second review: %s vs %s", again, reviewID)
	}
	// The review is actionable: ignoring it settles the source.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM household_member WHERE household_id=$1 LIMIT 1`, f.householdID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewdomain.ResolveDocumentReview(ctx, tx, reviewdomain.DocumentReviewCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, DocumentID: f.documentID, ActorType: "USER", Action: "IGNORE"}); err != nil {
		t.Fatalf("document review is not resolvable: %v", err)
	}
}
