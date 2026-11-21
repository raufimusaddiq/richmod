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

type identityGateway struct {
	t             *testing.T
	extraction    screenshotExtraction
	repairPrompts []string
	extractions   int
	classifyAs    string
}

func (g *identityGateway) NativeToolCall(_ context.Context, _ string, prompt string, _ any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	switch tools[0].Name {
	case "extract_transaction_screenshot":
		g.extractions++
		args, err := json.Marshal(g.extraction)
		return gateway.ToolCall{Name: "extract_transaction_screenshot", Arguments: args}, gateway.Metadata{Model: "synthetic"}, err
	case "classify_financial_document":
		args, err := json.Marshal(map[string]any{"document_type": g.classifyAs, "confidence": .98, "reason": "synthetic"})
		return gateway.ToolCall{Name: "classify_financial_document", Arguments: args}, gateway.Metadata{Model: "synthetic"}, err
	case "repair_transaction_fields":
		g.repairPrompts = append(g.repairPrompts, prompt)
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("repair unavailable")
	}
	g.t.Fatalf("unexpected tool %s", tools[0].Name)
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func runIdentityScreenshot(t *testing.T, label string, extraction screenshotExtraction) (screenshotFixture, *identityGateway, *Processor) {
	t.Helper()
	return runTypedScreenshot(t, label, "EWALLET_SCREENSHOT", extraction)
}

func runTypedScreenshot(t *testing.T, label, documentType string, extraction screenshotExtraction) (screenshotFixture, *identityGateway, *Processor) {
	t.Helper()
	f := seedScreenshotFixture(t, label)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE document SET document_type=$2 WHERE id=$1`, f.documentID, documentType); err != nil {
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
	gw := &identityGateway{t: t, extraction: extraction, classifyAs: documentType}
	p := &Processor{pool: f.pool, gateway: gw, storage: storage}
	if err := p.ProcessScreenshot(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	return f, gw, p
}

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
	if err := p.ProcessScreenshot(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	if again := check(); again != reviewID {
		t.Fatalf("retry opened a second review: %s vs %s", again, reviewID)
	}
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

func TestUnpaidInvoiceOpensNoReview(t *testing.T) {
	f, _, _ := runTypedScreenshot(t, "Unpaid invoice", "BILL_OR_INVOICE", screenshotExtraction{Confidence: .97, PaymentStatus: "UNPAID", Transactions: []screenshotRow{}})
	ctx := context.Background()
	var document, source string
	var reviews, proposals int
	var validated bool
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM review_item WHERE household_id=s.household_id AND (document_id=d.id OR source_event_id=s.id)),
		(SELECT count(*) FROM transaction_proposal WHERE source_event_id=s.id),(SELECT validated FROM document_extraction WHERE document_id=d.id AND stage='TRANSACTION_SCREENSHOT')
		FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source, &reviews, &proposals, &validated); err != nil {
		t.Fatal(err)
	}
	if document != "FAILED" || source != "FAILED" || reviews != 0 || proposals != 0 || validated {
		t.Fatalf("document=%s source=%s reviews=%d proposals=%d validated=%v", document, source, reviews, proposals, validated)
	}
}

func TestReprocessFromScreenshotReviewRerunsTheDocument(t *testing.T) {
	at := time.Now().In(jakarta()).Add(-time.Hour).Format(time.RFC3339)
	bad := incidentRow(at)
	bad.TransactionIdentifiers = []screenshotIdentifier{{"ACCOUNT_NUMBER", "123456"}}
	f, gw, p := runIdentityScreenshot(t, "Identity reprocess", screenshotExtraction{Confidence: .99, Transactions: []screenshotRow{bad}})
	ctx := context.Background()
	var reviewID, userID string
	if err := f.pool.QueryRow(ctx, `SELECT id FROM review_item WHERE household_id=$1 AND document_id=$2 AND status='OPEN'`, f.householdID, f.documentID).Scan(&reviewID); err != nil {
		t.Fatalf("no open review before reprocess: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT user_id FROM household_member WHERE household_id=$1 LIMIT 1`, f.householdID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reviewdomain.ResolveDocumentReview(ctx, tx, reviewdomain.DocumentReviewCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, DocumentID: f.documentID, ActorType: "USER", Action: "REPROCESS_DOCUMENT"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) SELECT 'PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid) WHERE NOT EXISTS(SELECT 1 FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1::text AND status IN ('PENDING','RUNNING'))`, f.documentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	gw.extraction = screenshotExtraction{Confidence: .99, Transactions: []screenshotRow{incidentRow(at)}}
	extractionsBefore := gw.extractions
	if err := p.Process(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	var document, source string
	var jobs int
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM job WHERE type='PROCESS_TRANSACTION_SCREENSHOT' AND payload_json->>'document_id'=d.id::text) FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&document, &source, &jobs); err != nil {
		t.Fatal(err)
	}
	if document != "CLASSIFIED" || source != "PROCESSED" || jobs != 1 {
		t.Fatalf("reprocess left document=%s source=%s screenshot jobs=%d (stranded?)", document, source, jobs)
	}
	if err := p.ProcessScreenshot(ctx, f.documentID); err != nil {
		t.Fatal(err)
	}
	if gw.extractions != extractionsBefore+1 {
		t.Fatalf("the document was not re-extracted: %d -> %d", extractionsBefore, gw.extractions)
	}
	var amount, resolution, reviewStatus string
	var proposals, openReviews int
	var validated bool
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM transaction_proposal WHERE source_event_id=s.id),COALESCE((SELECT amount::text FROM transaction_proposal WHERE source_event_id=s.id LIMIT 1),''),
		(SELECT count(*) FROM review_item ri WHERE ri.household_id=s.household_id AND ri.status IN ('OPEN','PENDING_SEND') AND (ri.document_id=d.id OR ri.source_event_id=s.id OR ri.proposal_id IN (SELECT id FROM transaction_proposal WHERE source_event_id=s.id) OR ri.transaction_id IN (SELECT transaction_id FROM transaction_evidence WHERE source_event_id=s.id))),
		(SELECT status FROM review_item WHERE id=$2),(SELECT COALESCE(resolution_action,'') FROM review_item WHERE id=$2),
		(SELECT validated FROM document_extraction WHERE document_id=d.id AND stage='TRANSACTION_SCREENSHOT')
		FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID, reviewID).Scan(&document, &source, &proposals, &amount, &openReviews, &reviewStatus, &resolution, &validated); err != nil {
		t.Fatal(err)
	}
	if document == "FAILED" || source == "FAILED" || (source == "NEEDS_REVIEW" && openReviews == 0) || proposals != 1 || amount != "72001" {
		t.Fatalf("after reprocess document=%s source=%s proposals=%d amount=%s openReviews=%d", document, source, proposals, amount, openReviews)
	}
	if reviewStatus != "RESOLVED" || resolution != "REPROCESS_DOCUMENT" || !validated {
		t.Fatalf("review=%s/%s validatedExtraction=%v (the stale invalid extraction must be replaced)", reviewStatus, resolution, validated)
	}
}
