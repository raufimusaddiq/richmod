package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func sentCardCall(t *testing.T, fake *fakeTelegram, method string) fakeTelegramCall {
	t.Helper()
	calls := fake.recorded()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			return calls[i]
		}
	}
	t.Fatalf("no %s call in %+v", method, calls)
	return fakeTelegramCall{}
}

func messageBot(t *testing.T, messageID int) (*Bot, *fakeTelegram) {
	return newFakeTelegram(t, func(method string) (int, string) {
		return 200, fmt.Sprintf(`{"ok":true,"result":{"message_id":%d}}`, messageID)
	})
}

func (f *deliveryFixture) positionRefreshJobs(t *testing.T, chat int64) []queuedSend {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT id::text,payload_json::text FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND status='PENDING' AND payload_json->>'refresh_position'='true' AND payload_json->>'chat_id'=$1::bigint::text`, chat)
	mustAgentTest(t, err)
	defer rows.Close()
	var out []queuedSend
	for rows.Next() {
		var id, raw string
		mustAgentTest(t, rows.Scan(&id, &raw))
		out = append(out, queuedSend{JobID: id, Payload: SendPayload{Text: raw}})
	}
	mustAgentTest(t, rows.Err())
	return out
}

func TestOneLiveCardPerChatShowsItsPositionAndReleasesTheNextWithoutOne(t *testing.T) {
	t.Parallel()
	f := seedDeliveryFixture(t)
	first := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	second := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	if f.deliveryStatus(t, first, f.chatA) != "PENDING" || f.deliveryStatus(t, second, f.chatA) != "QUEUED" {
		t.Fatalf("one live card: first=%s second=%s", f.deliveryStatus(t, first, f.chatA), f.deliveryStatus(t, second, f.chatA))
	}
	bot, fake := messageBot(t, 4001)
	send := f.sends(t, first, f.chatA)[0]
	mustAgentTest(t, f.processor.DeliverSend(f.ctx, bot, send.JobID, f.householdID, send.Payload))
	if text := fmt.Sprint(sentCardCall(t, fake, "sendMessage").Body["text"]); !strings.HasSuffix(text, "\n\n1 dari 2") {
		t.Fatalf("the live card with one waiting must say 1 dari 2: %q", text)
	}
	var stored string
	mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT delivered_text FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2`, first, f.chatA).Scan(&stored))
	if strings.Contains(stored, "dari 2") {
		t.Fatalf("the stored card text must not carry the position: %q", stored)
	}

	f.resolve(t, first)
	if f.deliveryStatus(t, second, f.chatA) != "PENDING" {
		t.Fatal("finishing the live card must release the next one")
	}
	bot2, fake2 := messageBot(t, 4002)
	next := f.sends(t, second, f.chatA)[0]
	mustAgentTest(t, f.processor.DeliverSend(f.ctx, bot2, next.JobID, f.householdID, next.Payload))
	if text := fmt.Sprint(sentCardCall(t, fake2, "sendMessage").Body["text"]); strings.Contains(text, " dari ") {
		t.Fatalf("a card alone in its chat has no position line: %q", text)
	}
}

func TestABurstOfQueuedCardsEditsTheLiveCardPositionOnce(t *testing.T) {
	t.Parallel()
	f := seedDeliveryFixture(t)
	first := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	bot, fake := messageBot(t, 4101)
	send := f.sends(t, first, f.chatA)[0]
	mustAgentTest(t, f.processor.DeliverSend(f.ctx, bot, send.JobID, f.householdID, send.Payload))
	for i := 0; i < 4; i++ {
		f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	}
	jobs := f.positionRefreshJobs(t, f.chatA)
	if len(jobs) != 1 {
		t.Fatalf("a burst of four queued cards queued %d position edits, want 1", len(jobs))
	}
	edit, err := DecodeEditPayload(json.RawMessage(jobs[0].Payload.Text))
	mustAgentTest(t, err)
	mustAgentTest(t, f.processor.DeliverEdit(f.ctx, bot, jobs[0].JobID, f.householdID, edit))
	call := sentCardCall(t, fake, "editMessageText")
	if text := fmt.Sprint(call.Body["text"]); !strings.HasSuffix(text, "\n\n1 dari 5") {
		t.Fatalf("position edit = %q", text)
	}
	if !reflect.DeepEqual(call.Body["reply_markup"], sentCardCall(t, fake, "sendMessage").Body["reply_markup"]) {
		t.Fatalf("the position edit changed the buttons: %v", call.Body["reply_markup"])
	}
}

func TestPositionEditLeavesACardWithAnUnknownKeyboardAlone(t *testing.T) {
	t.Parallel()
	f := seedDeliveryFixture(t)
	first := f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	bot, fake := messageBot(t, 4201)
	send := f.sends(t, first, f.chatA)[0]
	mustAgentTest(t, f.processor.DeliverSend(f.ctx, bot, send.JobID, f.householdID, send.Payload))
	_, err := f.pool.Exec(f.ctx, `UPDATE review_request_message SET delivered_markup=NULL WHERE review_request_id=$1`, first)
	mustAgentTest(t, err)
	f.enqueue(t, f.transaction(t, "BANK_EMAIL"), f.chatA, 0)
	jobs := f.positionRefreshJobs(t, f.chatA)
	if len(jobs) != 1 {
		t.Fatalf("position edits queued = %d", len(jobs))
	}
	edit, err := DecodeEditPayload(json.RawMessage(jobs[0].Payload.Text))
	mustAgentTest(t, err)
	mustAgentTest(t, f.processor.DeliverEdit(f.ctx, bot, jobs[0].JobID, f.householdID, edit))
	for _, call := range fake.recorded() {
		if call.Method == "editMessageText" {
			t.Fatalf("a card delivered before the keyboard was recorded lost its buttons: %+v", call.Body)
		}
	}
}

func TestOtherCopiesSayWhoFinishedTheCardAndWhere(t *testing.T) {
	t.Parallel()
	f := seedDeliveryFixture(t)
	retire := func(requestID string, chat, message int64) string {
		var recipient string
		mustAgentTest(t, f.pool.QueryRow(f.ctx, `SELECT id::text FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2`, requestID, chat).Scan(&recipient))
		bot, fake := newFakeTelegram(t, nil)
		mustAgentTest(t, f.processor.RetireReviewCard(f.ctx, bot, RetireCardPayload{ReviewRequestID: requestID, RecipientID: recipient, ChatID: chat, MessageID: message, Status: "RESOLVED"}))
		calls := fake.recorded()
		if len(calls) != 1 {
			t.Fatalf("retire calls = %+v", calls)
		}
		return fmt.Sprint(calls[0].Body["text"])
	}
	card := func(sourceType string, messageA, messageB int64) string {
		requestID := f.enqueue(t, f.transaction(t, sourceType), f.chatA, 0)
		mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatA, messageA, "🟡 Perlu ditinjau"))
		mustAgentTest(t, f.processor.BindReviewMessage(f.ctx, requestID, f.chatB, messageB, "🟡 Perlu ditinjau"))
		return requestID
	}

	inTelegram := card("BANK_EMAIL", 8101, 8102)
	_, err := f.pool.Exec(f.ctx, `UPDATE review_conversation SET context_json=context_json||'{"done_line":"✅ Tersimpan: Makan · Rp42.000"}'::jsonb WHERE review_request_id=$1`, inTelegram)
	mustAgentTest(t, err)
	f.resolve(t, inTelegram)
	if text := retire(inTelegram, f.chatB, 8102); !strings.HasSuffix(text, "Sudah diselesaikan oleh Rina di Telegram.\n✅ Tersimpan: Makan · Rp42.000") {
		t.Fatalf("telegram closing note = %q", text)
	}

	onWeb := card("BANK_EMAIL", 8201, 8202)
	tx, err := f.pool.Begin(f.ctx)
	mustAgentTest(t, err)
	_, err = tx.Exec(f.ctx, `SET LOCAL application_name='richmod-api'`)
	mustAgentTest(t, err)
	_, err = tx.Exec(f.ctx, `UPDATE review_item ri SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='CONFIRM_REVIEW' FROM review_request r WHERE r.id=$1 AND ri.id=r.review_item_id`, onWeb, f.userB)
	mustAgentTest(t, err)
	mustAgentTest(t, tx.Commit(f.ctx))
	for _, copy := range []struct {
		chat, message int64
	}{{f.chatA, 8201}, {f.chatB, 8202}} {
		if text := retire(onWeb, copy.chat, copy.message); !strings.Contains(text, "Sudah diselesaikan oleh Budi di aplikasi.") {
			t.Fatalf("web closing note on chat %d = %q", copy.chat, text)
		}
	}

	bySystem := card("BANK_EMAIL", 8301, 8302)
	_, err = f.pool.Exec(f.ctx, `UPDATE review_item ri SET status='RESOLVED',resolved_at=now(),resolution_action='NO_LONGER_APPLICABLE' FROM review_request r WHERE r.id=$1 AND ri.id=r.review_item_id`, bySystem)
	mustAgentTest(t, err)
	if text := retire(bySystem, f.chatB, 8302); !strings.HasSuffix(text, "Sudah diselesaikan otomatis.") {
		t.Fatalf("system closing note = %q", text)
	}
}

type ubahScene struct {
	f         *cardFixture
	request   string
	item      string
	tx        string
	other     string
	messageID int64
}

func newUbahScene(t *testing.T) *ubahScene {
	t.Helper()
	f := newCardFixture(t)
	first := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Galon Isi Ulang','galon') RETURNING id`, f.household)
	other := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Makan Siang','makan-siang') RETURNING id`, f.household)
	merchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, f.household)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',23000,now(),$2) RETURNING id`, f.household, merchant)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	scene := &ubahScene{f: f, item: item, tx: transaction, other: other, messageID: 17}
	scene.request = f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", scene.messageID, "🟡 Kategori belum pasti")
	f.exec(`INSERT INTO review_request_message(household_id,review_request_id,recipient_id,telegram_chat_id,telegram_message_id) SELECT $1,review_request_id,id,telegram_chat_id,telegram_message_id FROM review_request_recipient WHERE review_request_id=$2`, f.household, scene.request)
	f.tap(17, "review:cat:"+first, "🟡 Kategori belum pasti")
	f.tap(17, "review:once", "question")
	return scene
}

func TestUbahReopensTheSavedCardAndCorrectsItWithoutASecondTransaction(t *testing.T) {
	t.Parallel()
	s := newUbahScene(t)
	f := s.f
	text, markup := f.lastEdit(17)
	if !strings.HasPrefix(text, "✅ Tersimpan: Tins Mart · Galon Isi Ulang") || !strings.Contains(markup, "review:ubah") {
		t.Fatalf("saved card = %q %s", text, markup)
	}
	if f.count(`SELECT count(*) FROM job WHERE type='EXPIRE_UBAH_BUTTON' AND status='PENDING' AND run_after>now()+interval '9 minutes' AND payload_json->>'review_request_id'=$1`, s.request) != 1 {
		t.Fatal("the Ubah button must expire through one scheduled job about ten minutes out")
	}

	f.tap(17, "review:ubah", text)
	chooser, markup := f.lastEdit(17)
	if !strings.Contains(chooser, "Mau ubah apa?") || !strings.Contains(markup, "review:ubah:cat") {
		t.Fatalf("chooser = %q %s", chooser, markup)
	}
	if f.count(`SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND event_type='INTERACTION' AND action='UBAH_REOPENED'`, f.household) != 1 {
		t.Fatal("a reopen with Ubah must be recorded once")
	}
	f.tap(17, "review:ubah:cat", chooser)
	if _, markup = f.lastEdit(17); !strings.Contains(markup, "review:cat:"+s.other) {
		t.Fatalf("category step = %s", markup)
	}
	f.tap(17, "review:cat:"+s.other, chooser)
	text, markup = f.lastEdit(17)
	if !strings.HasPrefix(text, "✅ Tersimpan: Tins Mart · Makan Siang") || !strings.Contains(markup, "review:ubah") {
		t.Fatalf("card after the change = %q %s", text, markup)
	}
	var category string
	f.row(`SELECT category_id::text FROM transaction WHERE id=$1`, []any{&category}, s.tx)
	if category != s.other || f.count(`SELECT count(*) FROM transaction WHERE household_id=$1`, f.household) != 1 {
		t.Fatalf("the change must land on the same transaction: category=%s", category)
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_id=$2 AND action='EDIT_TRANSACTION' AND after_json->>'category_id'=$3`, f.household, s.tx, s.other) != 1 {
		t.Fatal("the change must be audited once")
	}
}

func TestUbahAfterTheWindowAnswersAndNeverMutates(t *testing.T) {
	t.Parallel()
	s := newUbahScene(t)
	f := s.f
	text, _ := f.lastEdit(17)
	f.exec(`UPDATE review_conversation SET context_json=jsonb_set(context_json,'{ubah,until}',to_jsonb(to_char(now() at time zone 'utc'-interval '1 minute','YYYY-MM-DD"T"HH24:MI:SS"Z"'))) WHERE review_request_id=$1`, s.request)
	sendsBefore := f.count(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text'='Sudah lewat 10 menit. Ubah di aplikasi.'`)
	var before string
	f.row(`SELECT category_id::text FROM transaction WHERE id=$1`, []any{&before}, s.tx)

	f.tap(17, "review:ubah", text)
	f.tap(17, "review:cat:"+s.other, text)
	if f.count(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text'='Sudah lewat 10 menit. Ubah di aplikasi.'`) != sendsBefore+2 {
		t.Fatal("a tap after the window must say it is too late")
	}
	if _, markup := f.lastEdit(17); strings.Contains(markup, "review:") {
		t.Fatalf("the button must be gone: %s", markup)
	}
	var after string
	f.row(`SELECT category_id::text FROM transaction WHERE id=$1`, []any{&after}, s.tx)
	if after != before || f.count(`SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='EDIT_TRANSACTION'`, s.tx) != 0 || f.count(`SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND action='UBAH_REOPENED'`, f.household) != 0 {
		t.Fatal("a tap after the window must change nothing")
	}
}

func TestUbahExpiryJobRemovesTheButton(t *testing.T) {
	t.Parallel()
	s := newUbahScene(t)
	f := s.f
	var raw string
	f.row(`SELECT payload_json::text FROM job WHERE type='EXPIRE_UBAH_BUTTON' AND payload_json->>'review_request_id'=$1`, []any{&raw}, s.request)
	payload, err := DecodeUbahExpirePayload(json.RawMessage(raw))
	mustAgentTest(t, err)
	mustAgentTest(t, NewProcessor(f.pool, nil).ExpireUbahButton(context.Background(), payload))
	text, markup := f.lastEdit(17)
	if !strings.HasPrefix(text, "✅ Tersimpan: Tins Mart · Galon Isi Ulang") || strings.Contains(markup, "review:") {
		t.Fatalf("expired card = %q %s", text, markup)
	}
}
