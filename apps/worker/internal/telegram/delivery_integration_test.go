package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// deliveryFixture is a two-member household: Rina (the owner, chatA) and Budi
// (chatB), both linked to Telegram.
type deliveryFixture struct {
	pool                   *pgxpool.Pool
	householdID            string
	userA, userB           string
	chatA, chatB           int64
	processor              *Processor
	categoryID             string
	ctx                    context.Context
	stamp                  int64
	transactionSourceCount int
}

func seedDeliveryFixture(t *testing.T) *deliveryFixture {
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
	stamp := time.Now().UnixNano()
	f := &deliveryFixture{pool: pool, ctx: ctx, stamp: stamp, chatA: stamp, chatB: stamp + 1}
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Delivery %d", stamp)).Scan(&f.householdID))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Rina','unused') RETURNING id`, fmt.Sprintf("delivery-a-%d@example.test", stamp)).Scan(&f.userA))
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Budi','unused') RETURNING id`, fmt.Sprintf("delivery-b-%d@example.test", stamp)).Scan(&f.userB))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER')`, f.householdID, f.userA, f.userB)
	mustAgentTest(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3),($4,$2,$5)`, f.chatA, f.householdID, f.userA, f.chatB, f.userB)
	mustAgentTest(t, err)
	mustAgentTest(t, pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, f.householdID).Scan(&f.categoryID))
	f.processor = NewProcessor(pool, boundReviewGateway{})
	return f
}

// transaction inserts a NEEDS_REVIEW expense whose only evidence is a source
// event of sourceType.
func (f *deliveryFixture) transaction(t *testing.T, sourceType string) string {
	t.Helper()
	f.transactionSourceCount++
	var transactionID, sourceID string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',42000,now(),CASE WHEN $2 LIKE 'TELEGRAM%' THEN $3::uuid END) RETURNING id`, f.householdID, sourceType, f.userA).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),$4,'PROCESSED') RETURNING id`, f.householdID, sourceType, fmt.Sprintf("delivery-%d-%d", f.stamp, f.transactionSourceCount), []byte(fmt.Sprint(f.stamp, f.transactionSourceCount))).Scan(&sourceID))
	_, err := f.pool.Exec(f.ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TEST')`, transactionID, sourceID)
	mustAgentTest(t, err)
	return transactionID
}

// enqueue projects the transaction's review as a producer answering chat's
// message replyTo would.
func (f *deliveryFixture) enqueue(t *testing.T, transactionID string, chat, replyTo int64) string {
	t.Helper()
	tx, err := f.pool.Begin(f.ctx)
	mustAgentTest(t, err)
	defer tx.Rollback(f.ctx)
	mustAgentTest(t, EnqueueReviewRequest(f.ctx, tx, transactionID, "UNKNOWN_PURPOSE", chat, replyTo, "🟡 Perlu ditinjau\n\nNominal: Rp42.000"))
	mustAgentTest(t, tx.Commit(f.ctx))
	var requestID string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&requestID))
	return requestID
}

type queuedSend struct {
	JobID   string
	Payload SendPayload
}

func (f *deliveryFixture) sends(t *testing.T, requestID string, chat int64) []queuedSend {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT id,payload_json::text FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1 AND payload_json->>'chat_id'=$2::bigint::text ORDER BY created_at,id`, requestID, chat)
	mustAgentTest(t, err)
	defer rows.Close()
	var out []queuedSend
	for rows.Next() {
		var id, raw string
		mustAgentTest(t, rows.Scan(&id, &raw))
		payload, err := DecodeSendPayload(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("queued card payload does not decode: %v (%s)", err, raw)
		}
		out = append(out, queuedSend{JobID: id, Payload: payload})
	}
	mustAgentTest(t, rows.Err())
	return out
}

func (f *deliveryFixture) deliveryStatus(t *testing.T, requestID string, chat int64) string {
	t.Helper()
	var status string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT delivery_status FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2`, requestID, chat).Scan(&status))
	return status
}

func (f *deliveryFixture) resolve(t *testing.T, requestID string) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `WITH item AS (UPDATE review_item ri SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='TELEGRAM_CONFIRM' FROM review_request r WHERE r.id=$1 AND ri.id=r.review_item_id RETURNING ri.transaction_id)
		UPDATE transaction SET status='CONFIRMED',confirmed_at=now(),category_id=$3 WHERE id IN (SELECT transaction_id FROM item)`, requestID, f.userA, f.categoryID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, requestID)
	mustAgentTest(t, err)
}

// R9: every member gets the card, but only the member whose message raised it
// gets a threaded copy; the spouse's copy names that member and threads to
// nothing. A household-level source threads no copy and names nobody.
func TestReviewCardThreadsOnlyTheOriginatingMember(t *testing.T) {
	f := seedDeliveryFixture(t)
	requestID := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 77)
	own, spouse := f.sends(t, requestID, f.chatA), f.sends(t, requestID, f.chatB)
	if len(own) != 1 || len(spouse) != 1 {
		t.Fatalf("every member must get one card: own=%d spouse=%d", len(own), len(spouse))
	}
	if own[0].Payload.ReplyToMessageID != 77 || strings.Contains(own[0].Payload.Text, "Dari ") {
		t.Fatalf("originating member's copy = %+v", own[0].Payload)
	}
	if spouse[0].Payload.ReplyToMessageID != 0 || !strings.HasSuffix(spouse[0].Payload.Text, "\n\nDari Rina") {
		t.Fatalf("spouse's copy must not thread and must name Rina: %+v", spouse[0].Payload)
	}

	// A bank email has no originating member, even though the producer passes
	// a member's chat to deliver to.
	bankRequest := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	for _, chat := range []int64{f.chatA, f.chatB} {
		sends := f.sends(t, bankRequest, chat)
		if len(sends) != 1 || sends[0].Payload.ReplyToMessageID != 0 || strings.Contains(sends[0].Payload.Text, "Dari ") {
			t.Fatalf("household-level card for chat %d = %+v", chat, sends)
		}
	}
}

// T4: a second producer run for a transaction whose review is already
// projected reuses that request and its cards.
func TestEnqueueReviewRequestReusesTheOpenProjection(t *testing.T) {
	f := seedDeliveryFixture(t)
	transactionID := f.transaction(t, "TELEGRAM_TEXT")
	requestID := f.enqueue(t, transactionID, f.chatA, 77)
	if again := f.enqueue(t, transactionID, f.chatA, 78); again != requestID {
		t.Fatalf("second producer run created request %s, want %s", again, requestID)
	}
	var requests int
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&requests))
	if requests != 1 || len(f.sends(t, requestID, f.chatA)) != 1 || len(f.sends(t, requestID, f.chatB)) != 1 {
		t.Fatalf("reuse sent a second card: requests=%d", requests)
	}
}

// R8 pacing: a chat holds at most three live review cards; a fourth waits
// QUEUED and is released when an earlier one closes. The expiry sweep closes
// stale projections, retires their cards, and leaves the review open.
func TestReviewCardPacingAndExpirySweep(t *testing.T) {
	f := seedDeliveryFixture(t)
	var requests []string
	for i := 0; i < 4; i++ {
		requests = append(requests, f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, int64(100+i)))
	}
	for i, requestID := range requests[:3] {
		if status := f.deliveryStatus(t, requestID, f.chatA); status != "PENDING" || len(f.sends(t, requestID, f.chatA)) != 1 {
			t.Fatalf("card %d should be sent: %s, %d sends", i, status, len(f.sends(t, requestID, f.chatA)))
		}
	}
	fourth := requests[3]
	if f.deliveryStatus(t, fourth, f.chatA) != "QUEUED" || len(f.sends(t, fourth, f.chatA)) != 0 {
		t.Fatal("the fourth card must wait for a free slot")
	}
	// The sweep never releases past the limit.
	if _, _, err := f.processor.SweepReviewProjections(f.ctx, 100); err != nil || f.deliveryStatus(t, fourth, f.chatA) != "QUEUED" {
		t.Fatalf("sweep released a card with no free slot: %v", err)
	}
	f.resolve(t, requests[0])
	if f.deliveryStatus(t, fourth, f.chatA) != "PENDING" || len(f.sends(t, fourth, f.chatA)) != 1 {
		t.Fatal("closing a card must release the oldest queued card")
	}

	// Expire the second card's projection: the sweep closes it, queues its
	// retirement, and the canonical item stays open.
	_, err := f.pool.Exec(f.ctx, `UPDATE review_request_recipient SET telegram_message_id=501,delivery_status='SENT' WHERE review_request_id=$1 AND telegram_chat_id=$2`, requests[1], f.chatA)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE review_request SET expires_at=now()-interval '1 minute' WHERE id=$1`, requests[1])
	mustAgentTest(t, err)
	expired, _, err := f.processor.SweepReviewProjections(f.ctx, 100)
	if err != nil || expired < 1 {
		t.Fatalf("sweep expired %d: %v", expired, err)
	}
	var requestStatus, itemStatus string
	var retirements int
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT r.status,ri.status,(SELECT count(*) FROM job j WHERE j.type='RETIRE_TELEGRAM_REVIEW_CARD' AND j.payload_json->>'review_request_id'=r.id::text AND j.payload_json->>'message_id'='501') FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, requests[1]).Scan(&requestStatus, &itemStatus, &retirements))
	if requestStatus != "EXPIRED" || (itemStatus != "OPEN" && itemStatus != "PENDING_SEND") || retirements != 1 {
		t.Fatalf("expired projection: request=%s item=%s retirements=%d", requestStatus, itemStatus, retirements)
	}
	if _, _, err := f.processor.SweepReviewProjections(f.ctx, 100); err != nil {
		t.Fatal(err)
	}
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1`, requests[1]).Scan(&retirements))
	if retirements != 1 {
		t.Fatalf("a second sweep retired the card again: %d jobs", retirements)
	}
}

// T3: Telegram accepted the card but binding failed. The retry binds the
// recorded message and never sends a second card.
func TestDeliverSendRetryAfterBindFailureDoesNotResend(t *testing.T) {
	f := seedDeliveryFixture(t)
	requestID := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 77)
	send := f.sends(t, requestID, f.chatA)[0]
	bot, fake := newFakeTelegram(t, func(method string) (int, string) {
		return 200, `{"ok":true,"result":{"message_id":901}}`
	})
	// Make the bind fail once: the recipient row is moved away.
	_, err := f.pool.Exec(f.ctx, `UPDATE review_request_recipient SET telegram_chat_id=-telegram_chat_id WHERE review_request_id=$1 AND telegram_chat_id=$2`, requestID, f.chatA)
	mustAgentTest(t, err)
	if err := f.processor.DeliverSend(f.ctx, bot, send.JobID, f.householdID, send.Payload); err == nil {
		t.Fatal("bind failure must fail the attempt")
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE review_request_recipient SET telegram_chat_id=-telegram_chat_id WHERE review_request_id=$1 AND telegram_chat_id=$2`, requestID, -f.chatA)
	mustAgentTest(t, err)
	if err := f.processor.DeliverSend(f.ctx, bot, send.JobID, f.householdID, send.Payload); err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, call := range fake.recorded() {
		if call.Method == "sendMessage" {
			sent++
		}
	}
	var bound int64
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT COALESCE(telegram_message_id,0) FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2`, requestID, f.chatA).Scan(&bound))
	if sent != 1 || bound != 901 || f.deliveryStatus(t, requestID, f.chatA) != "SENT" {
		t.Fatalf("sendMessage calls=%d bound=%d", sent, bound)
	}
}

// T2: a card that never reaches its recipient is recorded FAILED with the
// Telegram reason, frees its pacing slot, and leaves the review open.
func TestTerminalCardSendFailureIsRecordedAndReleasesTheSlot(t *testing.T) {
	f := seedDeliveryFixture(t)
	var requests []string
	for i := 0; i < 4; i++ {
		requests = append(requests, f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, int64(200+i)))
	}
	send := f.sends(t, requests[0], f.chatA)[0]
	cause := &APIError{Method: "sendMessage", StatusCode: 403, Description: "Forbidden: bot was blocked by the user"}
	tx, err := f.pool.Begin(f.ctx)
	mustAgentTest(t, err)
	mustAgentTest(t, ReviewDeliveryFailedTx(f.ctx, tx, send.Payload, cause))
	mustAgentTest(t, tx.Commit(f.ctx))
	var deliveryError, itemStatus string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT COALESCE(rr.delivery_error,''),ri.status FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1 AND rr.telegram_chat_id=$2`, requests[0], f.chatA).Scan(&deliveryError, &itemStatus))
	if f.deliveryStatus(t, requests[0], f.chatA) != "FAILED" || !strings.Contains(deliveryError, "bot was blocked") || (itemStatus != "OPEN" && itemStatus != "PENDING_SEND") {
		t.Fatalf("failed delivery: status=%s error=%q item=%s", f.deliveryStatus(t, requests[0], f.chatA), deliveryError, itemStatus)
	}
	if f.deliveryStatus(t, requests[3], f.chatA) != "PENDING" {
		t.Fatal("a failed card must free its pacing slot")
	}
}

// R3: a follow-up message becomes the recipient's current card; the message it
// replaced loses its buttons now, and closing the review retires both.
func TestFollowUpMessageSupersedesAndRetiresTheOlderCard(t *testing.T) {
	f := seedDeliveryFixture(t)
	requestID := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 77)
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatA, 610, "🟡 Perlu ditinjau"))
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatA, 611, "Pilih kategori pengeluaran:"))
	var raw string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT payload_json::text FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1 AND payload_json->>'message_id'='610'`, requestID).Scan(&raw))
	payload, err := DecodeRetireCardPayload(json.RawMessage(raw))
	mustAgentTest(t, err)
	if !payload.Superseded {
		t.Fatalf("superseded retirement payload = %s", raw)
	}
	bot, fake := newFakeTelegram(t, nil)
	mustAgentTest(t, f.processor.RetireReviewCard(f.ctx, bot, payload))
	calls := fake.recorded()
	if len(calls) != 1 || calls[0].Method != "editMessageText" || calls[0].Body["message_id"] != float64(610) || calls[0].Body["text"] != "🟡 Perlu ditinjau\n\n"+supersededCardNote {
		t.Fatalf("superseded card retirement = %+v", calls)
	}
	// The current card is not touched while the review is open.
	current := payload
	current.MessageID = 611
	mustAgentTest(t, f.processor.RetireReviewCard(f.ctx, bot, current))
	if len(fake.recorded()) != 1 {
		t.Fatal("the live card lost its buttons")
	}
	f.resolve(t, requestID)
	var messages []int64
	rows, err := f.pool.Query(f.ctx, `SELECT (payload_json->>'message_id')::bigint FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND status='PENDING' AND payload_json->>'review_request_id'=$1 AND payload_json->>'status'='RESOLVED' ORDER BY 1`, requestID)
	mustAgentTest(t, err)
	for rows.Next() {
		var id int64
		mustAgentTest(t, rows.Scan(&id))
		messages = append(messages, id)
	}
	rows.Close()
	if len(messages) != 1 || messages[0] != 611 {
		// 610 still has its superseded retirement queued; the close adds 611.
		t.Fatalf("close retirements = %v", messages)
	}
	var total int
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT count(DISTINCT payload_json->>'message_id') FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1`, requestID).Scan(&total))
	if total != 2 {
		t.Fatalf("every message of the review must be retired, got %d", total)
	}
	// Every member's copy shows the outcome.
	closing := RetireCardPayload{ReviewRequestID: requestID, RecipientID: payload.RecipientID, ChatID: f.chatA, MessageID: 611, Status: "RESOLVED"}
	bot2, fake2 := newFakeTelegram(t, nil)
	mustAgentTest(t, f.processor.RetireReviewCard(f.ctx, bot2, closing))
	if calls := fake2.recorded(); len(calls) != 1 || !strings.HasSuffix(fmt.Sprint(calls[0].Body["text"]), "✅ Tinjauan ini sudah selesai: dicatat sebagai Makan oleh Rina.") {
		t.Fatalf("resolved retirement = %+v", calls)
	}
}

// R7: "message is not modified" is a success; an uneditable live card is sent
// again with its buttons and bound, never as an unbound buttonless message.
func TestDeliverEditNotModifiedAndUneditableCard(t *testing.T) {
	f := seedDeliveryFixture(t)
	requestID := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 77)
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatA, 700, "🟡 Perlu ditinjau"))
	markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: "review:ignore"}}}}
	edit := EditPayload{ChatID: f.chatA, MessageID: 700, Text: "Pilih kategori pengeluaran:", ReplyMarkup: markup, ReviewRequestID: requestID}

	bot, fake := newFakeTelegram(t, func(method string) (int, string) {
		return 400, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified: specified new message content and reply markup are exactly the same"}`
	})
	if err := f.processor.DeliverEdit(f.ctx, bot, "", f.householdID, edit); err != nil {
		t.Fatalf("not modified must succeed: %v", err)
	}
	if calls := fake.recorded(); len(calls) != 1 || calls[0].Method != "editMessageText" {
		t.Fatalf("a double tap must not send a new message: %+v", calls)
	}

	var jobID string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `INSERT INTO job(type,payload_json,status) VALUES('EDIT_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'message_id',700,'text','x','review_request_id',$2::text),'RUNNING') RETURNING id`, f.chatA, requestID).Scan(&jobID))
	bot, fake = newFakeTelegram(t, func(method string) (int, string) {
		if method == "sendMessage" {
			return 200, `{"ok":true,"result":{"message_id":701}}`
		}
		return 400, `{"ok":false,"error_code":400,"description":"Bad Request: message can't be edited"}`
	})
	mustAgentTest(t, f.processor.DeliverEdit(f.ctx, bot, jobID, f.householdID, edit))
	calls := fake.recorded()
	if len(calls) < 2 || calls[1].Method != "sendMessage" || calls[1].Body["reply_markup"] == nil {
		t.Fatalf("uneditable card must be re-sent with its buttons: %+v", calls)
	}
	var bound int64
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT telegram_message_id FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2`, requestID, f.chatA).Scan(&bound))
	if bound != 701 {
		t.Fatalf("re-sent card is not bound: %d", bound)
	}
	// A retry of the same job binds the recorded message and sends nothing new.
	mustAgentTest(t, f.processor.DeliverEdit(f.ctx, bot, jobID, f.householdID, edit))
	sends := 0
	for _, call := range fake.recorded() {
		if call.Method == "sendMessage" {
			sends++
		}
	}
	if sends != 1 {
		t.Fatalf("retry re-sent the card: %d sends", sends)
	}

	// A plain edit (no review) whose message Telegram rejects fails clearly
	// instead of being replaced by a new message.
	bot, fake = newFakeTelegram(t, func(string) (int, string) {
		return 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`
	})
	err := f.processor.DeliverEdit(f.ctx, bot, "", f.householdID, EditPayload{ChatID: f.chatA, MessageID: 5, Text: "x"})
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("rejected plain edit must fail terminally: %v", err)
	}
	for _, call := range fake.recorded() {
		if call.Method == "sendMessage" {
			t.Fatal("a plain edit was replaced by an unbound message")
		}
	}
}

// T5 and R8: once a tap resolved the card, a second tap (another member's or a
// late double tap) is answered with the outcome, and an expired card is never
// renewed by a tap.
func TestLateTapIsAnsweredWithTheOutcomeAndNeverRenews(t *testing.T) {
	f := seedDeliveryFixture(t)
	requestID := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 77)
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatB, 801, "🟡 Perlu ditinjau"))
	f.resolve(t, requestID)
	tap := func(chat, message int64, data string) string {
		update := telegramUpdate{}
		update.Message.Chat.ID = chat
		update.Message.From.ID = chat
		update.Message.MessageID = message
		update.CallbackQuery = &struct {
			ID   string `json:"id"`
			Data string `json:"data"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Message struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		}{ID: fmt.Sprint("cb-", message), Data: data}
		update.CallbackQuery.From.ID = chat
		update.CallbackQuery.Message.Chat.ID = chat
		update.CallbackQuery.Message.MessageID = message
		raw, _ := json.Marshal(update)
		var sourceID string
		mustAgentTest(t, f.pool.QueryRow(f.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, f.householdID, fmt.Sprintf("late-tap-%d-%d", f.stamp, message), raw).Scan(&sourceID))
		_, err := f.pool.Exec(f.ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw)
		mustAgentTest(t, err)
		mustAgentTest(t, f.processor.Process(f.ctx, sourceID))
		var reply string
		mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1::bigint::text AND payload_json->>'reply_to_message_id'=$2::bigint::text ORDER BY created_at DESC LIMIT 1`, chat, message).Scan(&reply))
		return reply
	}
	if reply := tap(f.chatB, 801, "review:cat:"+f.categoryID); reply != "Tinjauan ini sudah dijawab: dicatat sebagai Makan oleh Rina." {
		t.Fatalf("late tap reply = %q", reply)
	}

	// An expired card that is still tapped (before the sweep stripped it) stays
	// expired.
	expiredRequest := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 78)
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, expiredRequest, f.chatA, 802, "🟡 Perlu ditinjau"))
	_, err := f.pool.Exec(f.ctx, `UPDATE review_request SET status='EXPIRED',expires_at=now()-interval '1 day' WHERE id=$1`, expiredRequest)
	mustAgentTest(t, err)
	_ = tap(f.chatA, 802, "review:edit")
	var status string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT status FROM review_request WHERE id=$1`, expiredRequest).Scan(&status))
	if status != "EXPIRED" {
		t.Fatalf("a tap renewed an expired projection: %s", status)
	}
}

// T5: a reply to an older message of a review that is still open points to the
// newest message instead of the generic stale copy.
func TestReplyToSupersededMessagePointsToTheNewestCard(t *testing.T) {
	f := seedDeliveryFixture(t)
	requestID := f.enqueue(t, f.transaction(t, "TELEGRAM_TEXT"), f.chatA, 77)
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatA, 900, "🟡 Perlu ditinjau"))
	mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatA, 901, "Pilih kategori pengeluaran:"))
	reply, err := answeredReviewReply(f.ctx, f.pool, f.householdID, f.chatA, 900)
	mustAgentTest(t, err)
	if reply != supersededReviewReply {
		t.Fatalf("reply to superseded message = %q", reply)
	}
	if reply, err = answeredReviewReply(f.ctx, f.pool, f.householdID, f.chatA, 901); err != nil || reply != "" {
		t.Fatalf("the live card has no stale answer: %q %v", reply, err)
	}
	f.resolve(t, requestID)
	if reply, err = answeredReviewReply(f.ctx, f.pool, f.householdID, f.chatA, 900); err != nil || reply != "Tinjauan ini sudah dijawab: dicatat sebagai Makan oleh Rina." {
		t.Fatalf("answered reply = %q %v", reply, err)
	}
	// Another household never sees this review's outcome.
	if reply, err = answeredReviewReply(f.ctx, f.pool, "00000000-0000-0000-0000-000000000000", f.chatA, 900); err != nil || reply != "" {
		t.Fatalf("cross-household lookup = %q %v", reply, err)
	}
}

// A member whose copy is still waiting for a pacing slot keeps it when the
// other members' copies expire: releasing it renews the projection.
func TestQueuedCopySurvivesExpiryOfTheOtherCopies(t *testing.T) {
	f := seedDeliveryFixture(t)
	var fillers []string
	for i := 0; i < 3; i++ {
		fillers = append(fillers, f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0))
	}
	// Only Budi's chat is full: Rina's filler copies are gone.
	_, err := f.pool.Exec(f.ctx, `UPDATE review_request_recipient SET delivery_status='SKIPPED' WHERE telegram_chat_id=$1 AND review_request_id=ANY($2::uuid[])`, f.chatA, fillers)
	mustAgentTest(t, err)
	requestID := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	if f.deliveryStatus(t, requestID, f.chatA) != "PENDING" || f.deliveryStatus(t, requestID, f.chatB) != "QUEUED" {
		t.Fatalf("setup: A=%s B=%s", f.deliveryStatus(t, requestID, f.chatA), f.deliveryStatus(t, requestID, f.chatB))
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE review_request SET expires_at=now()-interval '1 minute' WHERE id=$1`, requestID)
	mustAgentTest(t, err)
	if _, _, err := f.processor.SweepReviewProjections(f.ctx, 1000); err != nil {
		t.Fatal(err)
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT status FROM review_request WHERE id=$1`, requestID).Scan(&status))
	if status != "EXPIRED" || f.deliveryStatus(t, requestID, f.chatB) != "QUEUED" {
		t.Fatalf("expiry dropped the waiting copy: request=%s B=%s", status, f.deliveryStatus(t, requestID, f.chatB))
	}
	f.resolve(t, fillers[0])
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT status FROM review_request WHERE id=$1`, requestID).Scan(&status))
	if status != "OPEN" || f.deliveryStatus(t, requestID, f.chatB) != "PENDING" || len(f.sends(t, requestID, f.chatB)) != 1 {
		t.Fatalf("released copy: request=%s B=%s", status, f.deliveryStatus(t, requestID, f.chatB))
	}
}

// A release never waits for a request that another transaction is closing, so
// a close and a release in the same chat cannot deadlock.
func TestPacedReleaseDoesNotWaitForARequestBeingClosed(t *testing.T) {
	f := seedDeliveryFixture(t)
	var live []string
	for i := 0; i < 3; i++ {
		live = append(live, f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0))
	}
	waiting := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	if f.deliveryStatus(t, waiting, f.chatA) != "QUEUED" {
		t.Fatal("setup: the fourth card must wait")
	}
	// Another transaction is closing the waiting request and holds its row.
	closing, err := f.pool.Begin(f.ctx)
	mustAgentTest(t, err)
	defer closing.Rollback(f.ctx)
	_, err = closing.Exec(f.ctx, `SELECT 1 FROM review_request WHERE id=$1 FOR UPDATE`, waiting)
	mustAgentTest(t, err)

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		_, err := f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, live[0])
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatalf("closing a live card waited on the request being closed: %v", err)
	}
	if f.deliveryStatus(t, waiting, f.chatA) != "QUEUED" {
		t.Fatal("a request locked by its own close must not be released")
	}
	_, err = closing.Exec(f.ctx, `UPDATE review_request SET status='CANCELLED' WHERE id=$1`, waiting)
	mustAgentTest(t, err)
	mustAgentTest(t, closing.Commit(f.ctx))
	if f.deliveryStatus(t, waiting, f.chatA) != "SKIPPED" {
		t.Fatalf("cancelled queued copy = %s", f.deliveryStatus(t, waiting, f.chatA))
	}
}
