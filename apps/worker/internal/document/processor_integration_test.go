package document

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSING',117) RETURNING id`, householdID, fmt.Sprintf("telegram:document-failure:%d", stamp), []byte(fmt.Sprintf("document-failure-%d", stamp))).Scan(&sourceID); err != nil {
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
	if err = pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM review_item WHERE document_id=d.id AND status='OPEN'),(SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'reply_to_message_id'='117'),(SELECT count(*) FROM audit_log WHERE entity_id=s.id AND action='DOCUMENT_CLASSIFICATION_FAILED') FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, documentID).Scan(&documentStatus, &sourceStatus, &reviews, &replies, &audits); err != nil {
		t.Fatal(err)
	}
	if documentStatus != "NEEDS_REVIEW" || sourceStatus != "NEEDS_REVIEW" || reviews != 1 || replies != 1 || audits != 1 {
		t.Fatalf("document=%s source=%s reviews=%d replies=%d audits=%d", documentStatus, sourceStatus, reviews, replies, audits)
	}
}
