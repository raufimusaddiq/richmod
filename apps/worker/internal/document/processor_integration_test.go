package document

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

// TestTerminalTelegramDocumentFailureCreatesReviewAndReply pins the terminal
// failure path: it writes exactly one open review item and one audit row, and
// does not double-fire on a duplicate callback. DOCUMENT_CLASSIFICATION has no
// Telegram completion path until UIR-06, so it projects no card (fail closed)
// even when the source came from Telegram.
func TestTerminalTelegramDocumentFailureCreatesReviewAndReply(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	stamp := time.Now().UnixNano()
	var householdID, sourceID, attachmentID, documentID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Document failure %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSING',$4) RETURNING id`, householdID, fmt.Sprintf("telegram:document-failure:%d", stamp), []byte(fmt.Sprintf("document-failure-%d", stamp)), stamp).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,'{"message":{"chat":{"id":719809965}}}')`, sourceID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, householdID, []byte(fmt.Sprintf("hash-%d", stamp)), fmt.Sprintf("test/%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}

	processor := &Processor{pool: pool}
	if err = processor.HandleTerminalFailure(ctx, documentID, fmt.Errorf("LLM gateway returned no native tool call")); err != nil {
		t.Fatal(err)
	}
	if err = processor.HandleTerminalFailure(ctx, documentID, fmt.Errorf("duplicate terminal callback")); err != nil {
		t.Fatal(err)
	}

	var documentStatus, sourceStatus string
	var reviews, replies, audits int
	if err = pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM review_item WHERE document_id=d.id AND status='OPEN'),(SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'reply_to_message_id'=$2),(SELECT count(*) FROM audit_log WHERE entity_id=s.id AND action='DOCUMENT_CLASSIFICATION_FAILED') FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, documentID, fmt.Sprint(stamp)).Scan(&documentStatus, &sourceStatus, &reviews, &replies, &audits); err != nil {
		t.Fatal(err)
	}
	if documentStatus != "NEEDS_REVIEW" || sourceStatus != "NEEDS_REVIEW" || reviews != 1 || replies != 0 || audits != 1 {
		t.Fatalf("document=%s source=%s reviews=%d replies=%d audits=%d", documentStatus, sourceStatus, reviews, replies, audits)
	}
}

// TestTelegramDocumentProjectionIsIdempotent proves a source that already has an
// open projection is not double-sent: the second projection call is a no-op.
func TestTelegramDocumentProjectionIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	stamp := time.Now().UnixNano()
	var householdID, sourceID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Wealth prompt %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW',117) RETURNING id`, householdID, fmt.Sprintf("telegram:wealth-prompt:%d", stamp), []byte(fmt.Sprintf("wealth-prompt-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,'{"message":{"chat":{"id":719809965}}}')`, sourceID); err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err = pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','PENDING_SEND','{"missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"],"decisionClass":"EVIDENCE_GAP"}'::jsonb) RETURNING id`, householdID, sourceID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	projectOnce := func() error {
		tx, txErr := pool.Begin(ctx)
		if txErr != nil {
			return txErr
		}
		defer tx.Rollback(ctx)
		if txErr = workerTelegram.ProjectReviewItem(ctx, tx, householdID, itemID, 117, "", 719809965); txErr != nil {
			return txErr
		}
		return tx.Commit(ctx)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err = projectOnce(); err != nil {
			t.Fatal(err)
		}
	}
	var conversations int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_conversation c JOIN review_request r ON r.id=c.review_request_id JOIN review_item ri ON ri.id=r.review_item_id WHERE ri.id=$1`, itemID).Scan(&conversations); err != nil {
		t.Fatal(err)
	}
	if conversations != 1 {
		t.Fatalf("review_conversation rows = %d, want 1 (second projection must be a no-op)", conversations)
	}
}
