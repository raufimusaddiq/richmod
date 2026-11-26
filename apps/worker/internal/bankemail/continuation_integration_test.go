package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

var walkMessageSeq atomic.Int64

type walkOption func(*bankWalk)

type bankWalk struct {
	t         *testing.T
	ctx       context.Context
	pool      *pgxpool.Pool
	household string
	user      string
	chats     []int64
	itemID    string
	sourceID  string
	merchant  string
	memory    bool
	match     bool
	second    bool
	telegram  *workerTelegram.Processor
	completer *Processor
	texts     map[int64]string
	buttons   map[int64][]string
}

func withSecondChat() walkOption { return func(w *bankWalk) { w.second = true } }
func withMemory() walkOption     { return func(w *bankWalk) { w.merchant, w.memory = "Toko Contoh", true } }
func withMatch() walkOption      { return func(w *bankWalk) { w.merchant, w.match = "Toko Contoh", true } }

func newBankWalk(t *testing.T, options ...walkOption) *bankWalk {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	w := &bankWalk{t: t, ctx: ctx, pool: pool, texts: map[int64]string{}, buttons: map[int64][]string{}}
	for _, option := range options {
		option(w)
	}
	stamp := testsupport.UniqueStamp()
	w.chats = []int64{stamp}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Walk %d", stamp)).Scan(&w.household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("walk-%d@example.test", stamp)).Scan(&w.user))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, w.household, w.user)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp, w.household, w.user)
	must(err)
	if w.second {
		var other string
		must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','unused') RETURNING id`, fmt.Sprintf("walk-b-%d@example.test", stamp)).Scan(&other))
		_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, w.household, other)
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp+1, w.household, other)
		must(err)
		w.chats = append(w.chats, stamp+1)
	}
	for index, row := range [][3]string{{"Makan", "makan"}, {"Belanja", "belanja"}} {
		_, err = pool.Exec(ctx, `INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,$2,$3,$4)`, w.household, row[0], row[1], index+1)
		must(err)
	}
	var account, listener string
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening Contoh','BANK','FULL_LEDGER') RETURNING id`, w.household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank Contoh','notifikasi@bank.example.test',$2,$3) RETURNING id`, w.household, account, w.user).Scan(&listener))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, w.household, fmt.Sprintf("walk-bank-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&w.sourceID))
	_, err = pool.Exec(ctx, `INSERT INTO bank_email_event(source_event_id,listener_id,observed_sender,message_id,subject,email_date,body) VALUES($1,$2,'notifikasi@bank.example.test',$3,'Info rekening','','isi')`, w.sourceID, listener, fmt.Sprintf("m-%d", stamp))
	must(err)
	merchantJSON := "null"
	if w.merchant != "" {
		merchantJSON = `"` + w.merchant + `"`
	}
	extraction := `{"kind":"TRANSACTION","direction":"OUTGOING","channel":"QR","amount_idr":null,"transaction_at":null,"merchant":` + merchantJSON + `,"counterparty":null,"reference":null,"description":null,"missing_fields":["amount_idr","transaction_at"],"confidence":0.9}`
	_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','v1',$3::jsonb,'VALID')`, w.sourceID, listener, extraction)
	must(err)
	if w.memory {
		var merchantID, categoryID string
		must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Toko Contoh') RETURNING id`, w.household).Scan(&merchantID))
		must(pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug='makan'`, w.household).Scan(&categoryID))
		_, err = pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'Toko Contoh',$2,$3,true,true)`, w.household, merchantID, categoryID)
		must(err)
	}
	if w.match {
		var merchantID string
		must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Toko Contoh') RETURNING id`, w.household).Scan(&merchantID))
		_, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR',now(),$2,now())`, w.household, merchantID)
		must(err)
	}
	decision, _ := reviewdec.Preset("UNKNOWN_BANK_TEMPLATE", "source_event", w.sourceID)
	encoded, err := decision.JSON()
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN',$3::jsonb) RETURNING id`, w.household, w.sourceID, string(encoded)).Scan(&w.itemID))
	tx, err := pool.Begin(ctx)
	must(err)
	must(workerTelegram.ProjectReviewItem(ctx, tx, w.household, w.itemID, 0, "", w.chats[0]))
	must(tx.Commit(ctx))
	w.telegram = workerTelegram.NewProcessor(pool, nil)
	w.completer = &Processor{pool: pool}
	w.deliver()
	return w
}

func (w *bankWalk) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *bankWalk) deliver() {
	w.t.Helper()
	for {
		rows, err := w.pool.Query(w.ctx, `SELECT id::text,type,payload_json FROM job WHERE type IN ('SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE') AND status='PENDING' AND (payload_json->>'chat_id')::bigint=ANY($1) ORDER BY created_at,id`, w.chats)
		w.must(err)
		type queued struct {
			id, kind string
			raw      []byte
		}
		var jobs []queued
		for rows.Next() {
			var job queued
			w.must(rows.Scan(&job.id, &job.kind, &job.raw))
			jobs = append(jobs, job)
		}
		rows.Close()
		if len(jobs) == 0 {
			return
		}
		for _, job := range jobs {
			var payload struct {
				ChatID          int64  `json:"chat_id"`
				MessageID       int64  `json:"message_id"`
				Text            string `json:"text"`
				ReviewRequestID string `json:"review_request_id"`
				ReplyMarkup     *struct {
					Keyboard [][]struct {
						Text string `json:"text"`
						Data string `json:"callback_data"`
					} `json:"inline_keyboard"`
				} `json:"reply_markup"`
			}
			w.must(json.Unmarshal(job.raw, &payload))
			messageID := payload.MessageID
			if job.kind == "SEND_TELEGRAM_MESSAGE" {
				messageID = 30000 + walkMessageSeq.Add(1)
				_, err := w.pool.Exec(w.ctx, `INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3,$4)`, job.id, w.household, payload.ChatID, messageID)
				w.must(err)
				if payload.ReviewRequestID != "" {
					w.must(w.telegram.BindReviewMessage(w.ctx, payload.ReviewRequestID, payload.ChatID, messageID, payload.Text))
				}
			} else {
				w.must(w.telegram.RecordReviewCardText(w.ctx, payload.ChatID, messageID, payload.Text))
			}
			w.texts[messageID] = payload.Text
			w.buttons[messageID] = nil
			if payload.ReplyMarkup != nil {
				for _, row := range payload.ReplyMarkup.Keyboard {
					for _, button := range row {
						w.buttons[messageID] = append(w.buttons[messageID], button.Text+"|"+button.Data)
					}
				}
			}
			_, err := w.pool.Exec(w.ctx, `UPDATE job SET status='SUCCEEDED',finished_at=now() WHERE id=$1`, job.id)
			w.must(err)
		}
	}
}

func (w *bankWalk) card(chat int64) (int64, string, []string) {
	w.t.Helper()
	var messageID int64
	w.must(w.pool.QueryRow(w.ctx, `SELECT COALESCE((SELECT rr.telegram_message_id FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id WHERE rr.telegram_chat_id=$1 AND rr.telegram_message_id IS NOT NULL AND rr.delivery_status='SENT' ORDER BY r.created_at DESC,rr.created_at DESC LIMIT 1),0)`, chat).Scan(&messageID))
	return messageID, w.texts[messageID], w.buttons[messageID]
}

func (w *bankWalk) source(sourceType, label string, payload map[string]any, chat, messageID int64) string {
	w.t.Helper()
	raw, err := json.Marshal(payload)
	w.must(err)
	var id string
	w.must(w.pool.QueryRow(w.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id) VALUES($1,$2,$3,now(),$4,'RECEIVED',$5,$6) RETURNING id`, w.household, sourceType, fmt.Sprintf("%s-%d-%d", label, chat, messageID), []byte(fmt.Sprintf("%s-%d-%d", label, chat, messageID)), messageID, chat).Scan(&id))
	_, err = w.pool.Exec(w.ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, id, string(raw))
	w.must(err)
	return id
}

func (w *bankWalk) say(chat int64, text string, replyTo int64) {
	w.t.Helper()
	messageID := 40000 + walkMessageSeq.Add(1)
	message := map[string]any{"message_id": messageID, "date": time.Now().Unix(), "text": text, "from": map[string]any{"id": chat}, "chat": map[string]any{"id": chat}}
	if replyTo != 0 {
		message["reply_to_message"] = map[string]any{"message_id": replyTo}
	}
	id := w.source("TELEGRAM_TEXT", "walk-text", map[string]any{"message": message}, chat, messageID)
	w.must(w.telegram.ProcessAgent(w.ctx, id))
	w.deliver()
}

func (w *bankWalk) reply(chat int64, text string) {
	w.t.Helper()
	messageID, _, _ := w.card(chat)
	w.say(chat, text, messageID)
}

func (w *bankWalk) tap(chat, messageID int64, label string) {
	w.t.Helper()
	data := ""
	for _, button := range w.buttons[messageID] {
		if strings.HasPrefix(button, label+"|") {
			data = strings.TrimPrefix(button, label+"|")
		}
	}
	if data == "" {
		w.t.Fatalf("no %q button on message %d: %v\n%s", label, messageID, w.buttons[messageID], w.texts[messageID])
	}
	w.tapData(chat, messageID, data)
}

func (w *bankWalk) tapData(chat, messageID int64, data string) {
	w.t.Helper()
	message := map[string]any{"message_id": messageID, "from": map[string]any{"id": chat}, "chat": map[string]any{"id": chat}}
	callback := map[string]any{"id": "cb", "data": data, "from": map[string]any{"id": chat}, "message": map[string]any{"message_id": messageID, "text": w.texts[messageID], "chat": map[string]any{"id": chat}}}
	id := w.source("TELEGRAM_CALLBACK", fmt.Sprintf("walk-tap-%d", walkMessageSeq.Add(1)), map[string]any{"message": message, "callback_query": callback}, chat, messageID)
	w.must(w.telegram.Process(w.ctx, id))
	w.deliver()
}

func (w *bankWalk) tapCard(label string) {
	w.t.Helper()
	messageID, _, _ := w.card(w.chats[0])
	w.tap(w.chats[0], messageID, label)
}

func (w *bankWalk) queueFacts() {
	w.t.Helper()
	w.reply(w.chats[0], "54rb")
	w.tapCard("Waktu email")
}

func (w *bankWalk) runJob() {
	w.t.Helper()
	rows, err := w.pool.Query(w.ctx, `SELECT id::text,payload_json FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1 ORDER BY created_at`, w.itemID)
	w.must(err)
	type queued struct {
		id  string
		raw []byte
	}
	var jobs []queued
	for rows.Next() {
		var job queued
		w.must(rows.Scan(&job.id, &job.raw))
		jobs = append(jobs, job)
	}
	rows.Close()
	for _, job := range jobs {
		payload, err := DecodePayload(job.raw)
		w.must(err)
		w.must(w.completer.Complete(w.ctx, payload))
		_, err = w.pool.Exec(w.ctx, `UPDATE job SET status='SUCCEEDED',finished_at=now() WHERE id=$1`, job.id)
		w.must(err)
	}
	w.deliver()
}

func (w *bankWalk) count(sql string, args ...any) int {
	w.t.Helper()
	var n int
	w.must(w.pool.QueryRow(w.ctx, sql, args...).Scan(&n))
	return n
}

func (w *bankWalk) wantText(messageID int64, parts ...string) {
	w.t.Helper()
	for _, part := range parts {
		if !strings.Contains(w.texts[messageID], part) {
			w.t.Fatalf("message %d lacks %q:\n%s", messageID, part, w.texts[messageID])
		}
	}
}

func (w *bankWalk) wantButtons(messageID int64, labels ...string) {
	w.t.Helper()
	for _, label := range labels {
		found := false
		for _, button := range w.buttons[messageID] {
			found = found || strings.HasPrefix(button, label+"|")
		}
		if !found {
			w.t.Fatalf("message %d lacks the %q button: %v", messageID, label, w.buttons[messageID])
		}
	}
}

func (w *bankWalk) wantNoLooseSends(before int) {
	w.t.Helper()
	after := w.count(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=ANY($1)`, w.chats)
	if after != before {
		w.t.Fatalf("the completion sent %d new message(s); the card must continue in place", after-before)
	}
	if w.count(`SELECT count(*) FROM job j WHERE j.type='RETIRE_TELEGRAM_REVIEW_CARD' AND j.status='PENDING' AND (j.payload_json->>'chat_id')::bigint=ANY($1) AND EXISTS(SELECT 1 FROM review_request_recipient rr WHERE rr.telegram_chat_id=(j.payload_json->>'chat_id')::bigint AND rr.telegram_message_id=(j.payload_json->>'message_id')::bigint AND rr.delivery_status='SENT')`, w.chats) != 0 {
		w.t.Fatal("a pending retirement would strip the continued card")
	}
}

func (w *bankWalk) sends() int {
	return w.count(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=ANY($1)`, w.chats)
}

func (w *bankWalk) transactions() int {
	return w.count(`SELECT count(*) FROM transaction WHERE household_id=$1`, w.household)
}

func TestBankCardContinuesAfterCompletion(t *testing.T) {
	rows := []struct {
		name    string
		options []walkOption
		run     func(*bankWalk)
	}{
		{"the full walk: amount, email time, then merchant and category on the same message", nil, func(w *bankWalk) {
			w.queueFacts()
			cardID, _, _ := w.card(w.chats[0])
			w.wantText(cardID, "✅ Diterima: Rp54.000")
			before := w.sends()
			w.runJob()
			w.wantNoLooseSends(before)
			after, _, _ := w.card(w.chats[0])
			if after != cardID {
				t.Fatalf("the card moved from message %d to %d", cardID, after)
			}
			w.wantText(cardID, "Ini bayar ke mana?", "Nominal: Rp54.000")
			w.wantButtons(cardID, "Abaikan")
			w.reply(w.chats[0], "Warung Contoh")
			messageID, _, _ := w.card(w.chats[0])
			w.wantText(messageID, "✅ Warung Contoh disimpan. Masuk kategori apa?")
			w.tap(w.chats[0], messageID, "Makan")
			messageID, _, _ = w.card(w.chats[0])
			w.wantText(messageID, "Pakai Makan juga untuk transaksi berikutnya dari Warung Contoh?")
			w.tap(w.chats[0], messageID, "Sekali ini")
			messageID, _, _ = w.card(w.chats[0])
			w.wantText(messageID, "✅ Tersimpan: Warung Contoh · Makan · Rp54.000")
			if w.transactions() != 1 {
				t.Fatalf("transactions = %d, want 1", w.transactions())
			}
		}},
		{"a tap and a thread reply on the continued card reach the new review", nil, func(w *bankWalk) {
			w.queueFacts()
			bankCard, _, _ := w.card(w.chats[0])
			w.runJob()
			cardID, _, _ := w.card(w.chats[0])
			if cardID != bankCard {
				t.Fatalf("the review moved from message %d to %d", bankCard, cardID)
			}
			w.tap(w.chats[0], cardID, "Abaikan")
			w.wantText(cardID, "Diabaikan")
			if w.count(`SELECT count(*) FROM transaction WHERE household_id=$1 AND status='VOIDED'`, w.household) != 1 {
				t.Fatal("the tap did not reach the new review")
			}
		}},
		{"a thread reply to a note under the continued card is the merchant", nil, func(w *bankWalk) {
			w.queueFacts()
			bankCard, _, _ := w.card(w.chats[0])
			w.runJob()
			cardID, _, _ := w.card(w.chats[0])
			if cardID != bankCard {
				t.Fatalf("the review moved from message %d to %d", bankCard, cardID)
			}
			noteID := 50000 + walkMessageSeq.Add(1)
			var jobID string
			w.must(w.pool.QueryRow(w.ctx, `INSERT INTO job(type,payload_json,status) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text','Catatan.'),'SUCCEEDED') RETURNING id::text`, w.chats[0], cardID).Scan(&jobID))
			_, err := w.pool.Exec(w.ctx, `INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3,$4)`, jobID, w.household, w.chats[0], noteID)
			w.must(err)
			w.say(w.chats[0], "Warung Contoh", noteID)
			messageID, _, _ := w.card(w.chats[0])
			w.wantText(messageID, "Masuk kategori apa?")
		}},
		{"the other member's copy follows the card and keeps working", []walkOption{withSecondChat()}, func(w *bankWalk) {
			w.queueFacts()
			otherID, _, _ := w.card(w.chats[1])
			w.wantText(otherID, "✅ Diterima: Rp54.000")
			w.runJob()
			w.wantText(otherID, "Ini bayar ke mana?")
			if w.count(`SELECT count(*) FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE rr.telegram_chat_id=$1 AND rr.telegram_message_id=$2 AND r.status='OPEN' AND r.review_type='UNKNOWN_MERCHANT'`, w.chats[1], otherID) != 1 {
				t.Fatal("the other member's message is not bound to the open review")
			}
			w.say(w.chats[1], "Warung Contoh", otherID)
			messageID, _, _ := w.card(w.chats[1])
			w.wantText(messageID, "Masuk kategori apa?")
			ownerID, _, _ := w.card(w.chats[0])
			w.wantText(ownerID, "Masuk kategori apa?")
		}},
		{"a remembered merchant ends on the same card with one done line", []walkOption{withMemory()}, func(w *bankWalk) {
			w.queueFacts()
			cardID, _, _ := w.card(w.chats[0])
			before := w.sends()
			w.runJob()
			w.wantNoLooseSends(before)
			w.wantText(cardID, "✅ Tersimpan: Toko Contoh · Makan · Rp54.000 ·")
			if len(w.buttons[cardID]) != 0 {
				t.Fatalf("a finished card kept buttons: %v", w.buttons[cardID])
			}
			if w.count(`SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, w.household) != 1 {
				t.Fatal("the transaction was not confirmed")
			}
		}},
		{"an email that matches a recorded transaction says it was linked", []walkOption{withMatch()}, func(w *bankWalk) {
			w.queueFacts()
			cardID, _, _ := w.card(w.chats[0])
			before := w.sends()
			w.runJob()
			w.wantNoLooseSends(before)
			w.wantText(cardID, "✅ Digabung dengan transaksi yang sudah ada: Rp54.000 ·")
			if w.transactions() != 1 {
				t.Fatalf("transactions = %d, want the existing one only", w.transactions())
			}
		}},
		{"both members answer the last step: one job, the second records nothing, the job twice makes one transaction", []walkOption{withSecondChat()}, func(w *bankWalk) {
			w.reply(w.chats[0], "54rb")
			otherID, _, _ := w.card(w.chats[1])
			w.wantButtons(otherID, "Waktu email")
			otherTap := ""
			for _, button := range w.buttons[otherID] {
				if strings.HasPrefix(button, "Waktu email|") {
					otherTap = strings.TrimPrefix(button, "Waktu email|")
				}
			}
			w.tapCard("Waktu email")
			if w.count(`SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1`, w.itemID) != 1 {
				t.Fatal("the first answer did not queue exactly one job")
			}
			w.tapData(w.chats[1], otherID, otherTap)
			if w.count(`SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1`, w.itemID) != 1 {
				t.Fatal("the second answer queued another job")
			}
			if w.transactions() != 0 {
				t.Fatal("the second answer recorded something")
			}
			w.runJob()
			w.runJob()
			if w.transactions() != 1 || w.count(`SELECT count(*) FROM transaction_proposal WHERE source_event_id=$1`, w.sourceID) != 1 {
				t.Fatalf("running the job twice made %d transactions", w.transactions())
			}
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			w := newBankWalk(t, row.options...)
			row.run(w)
		})
	}
}
