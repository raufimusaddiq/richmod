package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type fakeTelegramCall struct {
	Method string
	Body   map[string]any
}

type fakeTelegram struct {
	mu      sync.Mutex
	calls   []fakeTelegramCall
	respond func(method string) (int, string)
}

func newFakeTelegram(t *testing.T, respond func(method string) (int, string)) (*Bot, *fakeTelegram) {
	t.Helper()
	fake := &fakeTelegram{respond: respond}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/bottoken/")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.mu.Lock()
		fake.calls = append(fake.calls, fakeTelegramCall{Method: method, Body: body})
		fake.mu.Unlock()
		status, response := http.StatusOK, `{"ok":true,"result":true}`
		if fake.respond != nil {
			status, response = fake.respond(method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	bot := NewBot("token")
	bot.base = server.URL
	return bot, fake
}

func (f *fakeTelegram) recorded() []fakeTelegramCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeTelegramCall(nil), f.calls...)
}

type retireFixture struct {
	pool                   *pgxpool.Pool
	householdID, requestID string
	chatA, chatB, chatC    int64
	recipientA, recipientB string
}

func seedRetireFixture(t *testing.T) retireFixture {
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
	stamp := testsupport.UniqueStamp()
	f := retireFixture{pool: pool, chatA: stamp, chatB: stamp + 1, chatC: stamp + 2}
	var transactionID, itemID string
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Retire cards %d", stamp)).Scan(&f.householdID))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',55000,now()) RETURNING id`, f.householdID).Scan(&transactionID))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,status) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, itemID, f.householdID, transactionID).Scan(&f.requestID))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id,delivered_text) VALUES($1,$2,31,'🟡 Perlu ditinjau') RETURNING id`, f.requestID, f.chatA).Scan(&f.recipientA))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,32) RETURNING id`, f.requestID, f.chatB).Scan(&f.recipientB))
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2)`, f.requestID, f.chatC)
	mustAgentTest(t, err)
	return f
}

func (f retireFixture) retireJobs(t *testing.T) []RetireCardPayload {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT payload_json::text,lane FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1 ORDER BY payload_json->>'message_id',created_at`, f.requestID)
	mustAgentTest(t, err)
	defer rows.Close()
	var payloads []RetireCardPayload
	for rows.Next() {
		var raw, lane string
		mustAgentTest(t, rows.Scan(&raw, &lane))
		if lane != "INTERACTIVE" {
			t.Fatalf("retire job lane=%s, want INTERACTIVE", lane)
		}
		payload, err := DecodeRetireCardPayload(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("trigger payload does not decode: %v (%s)", err, raw)
		}
		payloads = append(payloads, payload)
	}
	mustAgentTest(t, rows.Err())
	return payloads
}

func TestClosingReviewRequestQueuesOneRetirementPerDeliveredCard(t *testing.T) {
	t.Parallel()
	f := seedRetireFixture(t)
	ctx := context.Background()
	if jobs := f.retireJobs(t); len(jobs) != 0 {
		t.Fatalf("an open request queued %d retirements", len(jobs))
	}
	_, err := f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	jobs := f.retireJobs(t)
	if len(jobs) != 2 {
		t.Fatalf("resolved request queued %d retirements, want one per delivered card (2)", len(jobs))
	}
	if jobs[0].RecipientID != f.recipientA || jobs[0].ChatID != f.chatA || jobs[0].MessageID != 31 || jobs[0].Status != "RESOLVED" {
		t.Fatalf("first retirement = %+v", jobs[0])
	}
	if jobs[1].RecipientID != f.recipientB || jobs[1].ChatID != f.chatB || jobs[1].MessageID != 32 || jobs[1].Status != "RESOLVED" {
		t.Fatalf("second retirement = %+v", jobs[1])
	}
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `SELECT enqueue_review_card_retirement($1::uuid,NULL)`, f.requestID)
	mustAgentTest(t, err)
	if jobs := f.retireJobs(t); len(jobs) != 2 {
		t.Fatalf("retirement is not idempotent: %d jobs", len(jobs))
	}

	bot, fake := newFakeTelegram(t, nil)
	processor := NewProcessor(f.pool, nil)
	for _, job := range jobs {
		mustAgentTest(t, processor.RetireReviewCard(ctx, bot, job))
	}
	calls := fake.recorded()
	if len(calls) != 2 {
		t.Fatalf("retirement made %d Telegram calls, want 2: %+v", len(calls), calls)
	}
	if calls[0].Method != "editMessageText" || calls[0].Body["text"] != "🟡 Perlu ditinjau\n\n✅ Kartu ini sudah selesai." {
		t.Fatalf("card with known text = %+v", calls[0])
	}
	if calls[1].Method != "editMessageReplyMarkup" || calls[1].Body["text"] != nil {
		t.Fatalf("card without text = %+v", calls[1])
	}
	for _, call := range calls {
		markup, _ := call.Body["reply_markup"].(map[string]any)
		if keyboard, ok := markup["inline_keyboard"].([]any); !ok || len(keyboard) != 0 {
			t.Fatalf("retired card kept buttons: %+v", call.Body)
		}
		if call.Method == "sendMessage" {
			t.Fatal("retirement must never send a new message")
		}
	}
}

func TestRetirementSkipsRenewedRequestAndRetiresItsNextClose(t *testing.T) {
	t.Parallel()
	f := seedRetireFixture(t)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `UPDATE review_request SET status='EXPIRED' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='OPEN',expires_at=now()+interval '7 days' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='EXPIRED' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	jobs := f.retireJobs(t)
	if len(jobs) != 2 || jobs[0].Status != "EXPIRED" {
		t.Fatalf("expire/renew/expire queued %+v, want one EXPIRED retirement per card", jobs)
	}
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='OPEN',expires_at=now()+interval '7 days' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE review_request_recipient SET delivery_status='SENT',card_expires_at=now()+interval '7 days' WHERE review_request_id=$1 AND telegram_message_id IS NOT NULL`, f.requestID)
	mustAgentTest(t, err)
	bot, fake := newFakeTelegram(t, nil)
	processor := NewProcessor(f.pool, nil)
	for _, job := range jobs {
		mustAgentTest(t, processor.RetireReviewCard(ctx, bot, job))
	}
	if calls := fake.recorded(); len(calls) != 0 {
		t.Fatalf("a renewed request's cards were edited: %+v", calls)
	}
	_, err = f.pool.Exec(ctx, `UPDATE job SET status='SUCCEEDED',finished_at=now() WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1`, f.requestID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	jobs = f.retireJobs(t)
	var resolved []RetireCardPayload
	for _, job := range jobs {
		if job.Status == "RESOLVED" {
			resolved = append(resolved, job)
		}
	}
	if len(resolved) != 2 {
		t.Fatalf("closing a renewed request queued %d new retirements, want 2", len(resolved))
	}
	mustAgentTest(t, processor.RetireReviewCard(ctx, bot, resolved[0]))
	if calls := fake.recorded(); len(calls) != 1 || calls[0].Method != "editMessageText" {
		t.Fatalf("closed renewed card was not retired: %+v", calls)
	}
}

func TestCardBoundAfterCloseIsRecordedAndRetired(t *testing.T) {
	t.Parallel()
	f := seedRetireFixture(t)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	processor := NewProcessor(f.pool, nil)
	mustAgentTest(t, processor.BindReviewMessage(ctx, f.requestID, f.chatC, 33, "  Kartu terlambat  "))
	var messageID int64
	var text, status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT rr.telegram_message_id,rr.delivered_text,r.status FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id WHERE rr.review_request_id=$1 AND rr.telegram_chat_id=$2`, f.requestID, f.chatC).Scan(&messageID, &text, &status))
	if messageID != 33 || text != "Kartu terlambat" || status != "RESOLVED" {
		t.Fatalf("late card message=%d text=%q request=%s", messageID, text, status)
	}
	jobs := f.retireJobs(t)
	var late []RetireCardPayload
	for _, job := range jobs {
		if job.MessageID == 33 {
			late = append(late, job)
		}
	}
	if len(jobs) != 3 || len(late) != 1 || late[0].ChatID != f.chatC {
		t.Fatalf("late card retirements=%+v (all=%d), want exactly one for message 33", late, len(jobs))
	}
	mustAgentTest(t, processor.BindReviewMessage(ctx, f.requestID, f.chatC, 33, "Kartu terlambat"))
	if jobs := f.retireJobs(t); len(jobs) != 3 {
		t.Fatalf("rebinding the late card queued a duplicate retirement: %d", len(jobs))
	}
	bot, fake := newFakeTelegram(t, nil)
	mustAgentTest(t, processor.RetireReviewCard(ctx, bot, late[0]))
	calls := fake.recorded()
	if len(calls) != 1 || calls[0].Method != "editMessageText" || calls[0].Body["text"] != "Kartu terlambat\n\n✅ Kartu ini sudah selesai." {
		t.Fatalf("late card retirement = %+v", calls)
	}
}

func TestBindReviewMessageOpensPendingRequestWithoutRetiring(t *testing.T) {
	t.Parallel()
	f := seedRetireFixture(t)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `UPDATE review_request SET status='PENDING_SEND' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	mustAgentTest(t, NewProcessor(f.pool, nil).BindReviewMessage(ctx, f.requestID, f.chatC, 34, "Kartu"))
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, f.requestID).Scan(&status))
	if status != "OPEN" {
		t.Fatalf("bound pending request status=%s, want OPEN", status)
	}
	if jobs := f.retireJobs(t); len(jobs) != 0 {
		t.Fatalf("binding a live card queued %d retirements", len(jobs))
	}
}

func TestQueuedReviewEditIsDroppedAfterRetirement(t *testing.T) {
	t.Parallel()
	f := seedRetireFixture(t)
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	var update telegramUpdate
	update.Message.Chat.ID = f.chatA
	update.Message.MessageID = 31
	markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Kembali", CallbackData: "review:edit"}}}}
	mustAgentTest(t, enqueueReviewUpdateWithMarkup(ctx, tx, f.requestID, update, "Pilih detail yang ingin diubah:", markup))
	mustAgentTest(t, tx.Commit(ctx))
	var raw string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json::text FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1`, f.requestID).Scan(&raw))
	payload, err := DecodeEditPayload(json.RawMessage(raw))
	mustAgentTest(t, err)
	if payload.ReviewRequestID != f.requestID {
		t.Fatalf("review edit payload lost its request: %+v", payload)
	}
	processor := NewProcessor(f.pool, nil)
	live, err := processor.ReviewCardLive(ctx, payload.ReviewRequestID)
	mustAgentTest(t, err)
	if !live {
		t.Fatal("an edit of an open card must apply")
	}
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='CANCELLED' WHERE id=$1`, f.requestID)
	mustAgentTest(t, err)
	live, err = processor.ReviewCardLive(ctx, payload.ReviewRequestID)
	mustAgentTest(t, err)
	if live {
		t.Fatal("an edit queued before the close must not restore the retired card's buttons")
	}
	mustAgentTest(t, processor.RecordReviewCardText(ctx, f.chatB, 32, "Pilih kategori pengeluaran:"))
	var text string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT delivered_text FROM review_request_recipient WHERE id=$1`, f.recipientB).Scan(&text))
	if text != "Pilih kategori pengeluaran:" {
		t.Fatalf("edited card text=%q", text)
	}
}
