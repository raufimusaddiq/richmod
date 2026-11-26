package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type typingFixture struct {
	household  string
	documentID string
	sourceID   string
	chat       int64
	calls      func() []int64
	processor  *Processor
	ctx        context.Context
}

func newTypingFixture(t *testing.T, sourceType string, chat int64) *typingFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var mu sync.Mutex
	var received []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64  `json:"chat_id"`
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/bottoken/sendChatAction" && body.Action == "typing" {
			mu.Lock()
			received = append(received, body.ChatID)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	bot := NewBot("token")
	bot.base = server.URL
	processor := NewProcessor(pool, nil)
	processor.SetBot(bot)
	processor.documentTyping.interval = 20 * time.Millisecond

	stamp := testsupport.UniqueStamp()
	f := &typingFixture{chat: chat, processor: processor, ctx: ctx}
	f.calls = func() []int64 {
		mu.Lock()
		defer mu.Unlock()
		return append([]int64(nil), received...)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Typing %d", stamp)).Scan(&f.household))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_chat_id) VALUES($1,$2,$3,now(),$4,'RECEIVED',NULLIF($5::bigint,0)) RETURNING id`, f.household, sourceType, fmt.Sprintf("typing:%d", stamp), []byte(fmt.Sprintf("typing-%d", stamp)), chat).Scan(&f.sourceID))
	var attachmentID string
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, f.household, []byte(fmt.Sprintf("hash-%d", stamp)), fmt.Sprintf("test/%d.jpg", stamp)).Scan(&attachmentID))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, f.household, f.sourceID, attachmentID).Scan(&f.documentID))
	return f
}

func (f *typingFixture) payloads() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"FETCH_TELEGRAM_IMAGE":           json.RawMessage(fmt.Sprintf(`{"source_event_id":%q,"file_id":"f","user_id":"u","telegram_user_id":1}`, f.sourceID)),
		"PROCESS_DOCUMENT":               json.RawMessage(fmt.Sprintf(`{"document_id":%q}`, f.documentID)),
		"PROCESS_PAYSLIP":                json.RawMessage(fmt.Sprintf(`{"document_id":%q}`, f.documentID)),
		"PROCESS_RECEIPT":                json.RawMessage(fmt.Sprintf(`{"document_id":%q}`, f.documentID)),
		"PROCESS_TRANSACTION_SCREENSHOT": json.RawMessage(fmt.Sprintf(`{"document_id":%q}`, f.documentID)),
	}
}

func TestDocumentChainJobsTypeInSourceChatUntilReturn(t *testing.T) {
	chat := testsupport.UniqueStamp()
	f := newTypingFixture(t, "TELEGRAM_IMAGE", chat)
	for jobType, payload := range f.payloads() {
		before := len(f.calls())
		stop := f.processor.StartDocumentTyping(f.ctx, jobType, f.household, payload)
		if got := f.calls(); len(got) <= before || got[len(got)-1] != chat {
			t.Fatalf("%s: typing not sent to the source chat: %v", jobType, got)
		}
		stop()
		time.Sleep(60 * time.Millisecond)
		settled := len(f.calls())
		time.Sleep(120 * time.Millisecond)
		if len(f.calls()) != settled {
			t.Fatalf("%s: typing continued after the job returned", jobType)
		}
	}

	f = newTypingFixture(t, "TELEGRAM_IMAGE", chat)
	f.processor.documentTyping.interval = time.Hour
	before := len(f.calls())
	payload := f.payloads()["PROCESS_DOCUMENT"]
	stopA := f.processor.StartDocumentTyping(f.ctx, "PROCESS_DOCUMENT", f.household, payload)
	stopB := f.processor.StartDocumentTyping(f.ctx, "PROCESS_DOCUMENT", f.household, payload)
	defer stopA()
	defer stopB()
	if sent := len(f.calls()) - before; sent != 1 {
		t.Fatalf("parallel pages sent %d indicators to one chat, want 1", sent)
	}
}

func TestDocumentChainJobsDoNotTypeForNonTelegramDocuments(t *testing.T) {
	f := newTypingFixture(t, "WEB_IMAGE", 0)
	for jobType, payload := range f.payloads() {
		stop := f.processor.StartDocumentTyping(f.ctx, jobType, f.household, payload)
		time.Sleep(60 * time.Millisecond)
		stop()
	}
	other := newTypingFixture(t, "TELEGRAM_IMAGE", testsupport.UniqueStamp())
	stop := other.processor.StartDocumentTyping(other.ctx, "PROCESS_DOCUMENT", f.household, other.payloads()["PROCESS_DOCUMENT"])
	stop()
	if got := append(f.calls(), other.calls()...); len(got) != 0 {
		t.Fatalf("typing sent for a document that is not a Telegram chat in this household: %v", got)
	}
}
