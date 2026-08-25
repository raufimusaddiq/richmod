package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestImageProcessorCreatesGenericDocumentIdempotently(t *testing.T) {
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
	var householdID, userID, sourceID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Worker image %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Image Worker','unused') RETURNING id`, fmt.Sprintf("worker-image-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("telegram:update:%d", stamp), []byte("unique-payload")).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,'{}')`, sourceID); err != nil {
		t.Fatal(err)
	}
	raw := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("file_id") != "" {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"ok":true,"result":{"file_path":"documents/image.png","file_size":%d}}`, len(raw))))
			return
		}
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	bot := NewBot("test-token")
	bot.base = server.URL
	bot.http = server.Client()
	processor, err := NewImageProcessor(pool, bot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := ImagePayload{SourceEventID: sourceID, FileID: "file-id", FileName: "image.png", MIMEType: "image/png", Caption: "receipt", TelegramUserID: 123456, UserID: userID}
	if err = processor.Process(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if err = processor.Process(ctx, payload); err != nil {
		t.Fatal(err)
	}
	var documents, jobs, audits int
	var status string
	if err = pool.QueryRow(ctx, `SELECT processing_status,(SELECT count(*) FROM document WHERE source_event_id=$1),(SELECT count(*) FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id' IN(SELECT id::text FROM document WHERE source_event_id=$1)),(SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='UPLOAD_DOCUMENT') FROM source_event WHERE id=$1`, sourceID).Scan(&status, &documents, &jobs, &audits); err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSING" || documents != 1 || jobs != 1 || audits != 1 {
		t.Fatalf("status=%s documents=%d jobs=%d audits=%d", status, documents, jobs, audits)
	}
}
