package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

var contractStampOffset atomic.Int64

type scriptedPicker struct {
	agentCalls int
	script     []string
	err        error
	tools      [][]gateway.ToolDefinition
	inputs     []any
}

func (g *scriptedPicker) NativeToolCall(_ context.Context, _ string, _ string, input any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.tools = append(g.tools, tools)
	g.inputs = append(g.inputs, input)
	if g.err != nil {
		return gateway.ToolCall{}, gateway.Metadata{}, g.err
	}
	option := "NONE"
	if len(g.script) >= len(g.tools) {
		option = g.script[len(g.tools)-1]
	}
	args, _ := json.Marshal(map[string]string{"option": option})
	return gateway.ToolCall{Name: tools[0].Name, Arguments: args}, gateway.Metadata{}, nil
}

func (g *scriptedPicker) AgentTurn(context.Context, string, gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.agentCalls++
	return gateway.AgentResponse{Text: "Baik."}, nil
}

type contractHarness struct {
	t        *testing.T
	ctx      context.Context
	f        agentIntegrationFixture
	p        *Processor
	picker   *scriptedPicker
	kind     string
	chatB    int64
	reviewID string
	txID     string
	checked  map[string]bool
	markups  map[int64][]string
	texts    map[int64]string
}

type contractOption func(*contractHarness)

func withExtraCategories(count int) contractOption {
	return func(h *contractHarness) {
		for index := 1; index <= count; index++ {
			h.exec(`INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,$2,$3,$4)`, h.f.householdID, fmt.Sprintf("Kategori %02d", index), fmt.Sprintf("kategori-%02d", index), 10+index)
		}
	}
}

func withSecondMember() contractOption {
	return func(h *contractHarness) {
		var userB string
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','unused') RETURNING id`, fmt.Sprintf("member-%d@example.test", h.chatB)).Scan(&userB))
		h.exec(`INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, h.f.householdID, userB)
		h.exec(`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, h.chatB, h.f.householdID, userB)
	}
}

func withRememberedMerchant() contractOption {
	return func(h *contractHarness) {
		var merchantID, categoryID string
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Warung Contoh') RETURNING id`, h.f.householdID).Scan(&merchantID))
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM category WHERE household_id=$1 AND slug='makan'`, h.f.householdID).Scan(&categoryID))
		h.exec(`INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'Warung Contoh',$2,$3,true,true)`, h.f.householdID, merchantID, categoryID)
	}
}

func newContractHarness(t *testing.T, kind string, options ...contractOption) *contractHarness {
	t.Helper()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "contract")
	picker := &scriptedPicker{}
	h := &contractHarness{t: t, ctx: ctx, f: f, p: NewProcessor(f.pool, picker), picker: picker, kind: kind,
		chatB: testsupport.UniqueStamp() + contractStampOffset.Add(1), checked: map[string]bool{}, markups: map[int64][]string{}, texts: map[int64]string{}}
	for index, row := range [][3]string{{"Makan", "makan"}, {"Transportasi", "transportasi"}, {"Belanja", "belanja"}} {
		h.exec(`INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,$2,$3,$4)`, f.householdID, row[0], row[1], index+1)
	}
	for _, option := range options {
		option(h)
	}
	h.openReview()
	return h
}

func (h *contractHarness) exec(sql string, args ...any) {
	h.t.Helper()
	_, err := h.f.pool.Exec(h.ctx, sql, args...)
	mustAgentTest(h.t, err)
}

func (h *contractHarness) openReview() {
	h.t.Helper()
	merchant := ""
	if h.kind == "AMBIGUOUS_CATEGORY" {
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Toko Contoh') RETURNING id`, h.f.householdID).Scan(&merchant))
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',25000,'IDR',now(),NULLIF($2,'')::uuid,$3) RETURNING id`, h.f.householdID, merchant, h.f.userID).Scan(&h.txID))
	facts := "Nominal: Rp25.000"
	if merchant != "" {
		facts += "\nMerchant: Toko Contoh"
	}
	tx, err := h.f.pool.Begin(h.ctx)
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, EnqueueReviewRequest(h.ctx, tx, h.txID, h.kind, h.f.chatID, 0, facts))
	mustAgentTest(h.t, tx.Commit(h.ctx))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, h.txID).Scan(&h.reviewID))
	h.deliver()
}

func (h *contractHarness) chats() []int64 { return []int64{h.f.chatID, h.chatB} }

func (h *contractHarness) cardMessages() []int64 {
	rows, err := h.f.pool.Query(h.ctx, `SELECT telegram_message_id FROM review_request_recipient WHERE review_request_id=$1 AND telegram_message_id IS NOT NULL
		UNION SELECT telegram_message_id FROM review_request_message WHERE review_request_id=$1`, h.reviewID)
	mustAgentTest(h.t, err)
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		mustAgentTest(h.t, rows.Scan(&id))
		out = append(out, id)
	}
	return out
}

func (h *contractHarness) audit() {
	h.t.Helper()
	rows, err := h.f.pool.Query(h.ctx, `SELECT id::text,type,payload_json FROM job WHERE type IN ('SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE') AND (payload_json->>'chat_id')::bigint=ANY($1) ORDER BY created_at,id`, h.chats())
	mustAgentTest(h.t, err)
	type queued struct {
		id, kind string
		payload  []byte
	}
	var fresh []queued
	for rows.Next() {
		var job queued
		mustAgentTest(h.t, rows.Scan(&job.id, &job.kind, &job.payload))
		if !h.checked[job.id] {
			fresh = append(fresh, job)
		}
	}
	rows.Close()
	cards := h.cardMessages()
	for _, job := range fresh {
		h.checked[job.id] = true
		var payload struct {
			ReviewRequestID string `json:"review_request_id"`
			MessageID       int64  `json:"message_id"`
			Text            string `json:"text"`
		}
		mustAgentTest(h.t, json.Unmarshal(job.payload, &payload))
		if strings.Contains(payload.Text, "Balas pesan ini") {
			h.t.Fatalf("loose prompt copy: %q", payload.Text)
		}
		switch job.kind {
		case "SEND_TELEGRAM_MESSAGE":
			if payload.ReviewRequestID == "" {
				h.t.Fatalf("a send without a bound card: %s", job.payload)
			}
		case "EDIT_TELEGRAM_MESSAGE":
			if !slices.Contains(cards, payload.MessageID) {
				h.t.Fatalf("an edit of a message that is not a card of this review: %s (cards %v)", job.payload, cards)
			}
		}
	}
}

func (h *contractHarness) absorb() {
	h.t.Helper()
	rows, err := h.f.pool.Query(h.ctx, `SELECT id::text FROM job WHERE type IN ('SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE') AND (payload_json->>'chat_id')::bigint=ANY($1)`, h.chats())
	mustAgentTest(h.t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		mustAgentTest(h.t, rows.Scan(&id))
		h.checked[id] = true
	}
}

func (h *contractHarness) deliver() {
	h.t.Helper()
	h.audit()
	for {
		rows, err := h.f.pool.Query(h.ctx, `SELECT id::text,type,payload_json FROM job WHERE type IN ('SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','RETIRE_TELEGRAM_REVIEW_CARD') AND status='PENDING' AND (payload_json->>'chat_id')::bigint=ANY($1) ORDER BY created_at,id`, h.chats())
		mustAgentTest(h.t, err)
		type queued struct {
			id, kind string
			payload  []byte
		}
		var jobs []queued
		for rows.Next() {
			var job queued
			mustAgentTest(h.t, rows.Scan(&job.id, &job.kind, &job.payload))
			jobs = append(jobs, job)
		}
		rows.Close()
		if len(jobs) == 0 {
			return
		}
		for _, job := range jobs {
			h.deliverOne(job.id, job.kind, job.payload)
			h.exec(`UPDATE job SET status='SUCCEEDED',finished_at=now() WHERE id=$1`, job.id)
		}
	}
}

func (h *contractHarness) deliverOne(jobID, kind string, raw []byte) {
	h.t.Helper()
	switch kind {
	case "SEND_TELEGRAM_MESSAGE":
		var payload SendPayload
		mustAgentTest(h.t, json.Unmarshal(raw, &payload))
		messageID := nextMessageID()
		h.exec(`INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3,$4)`, jobID, h.f.householdID, payload.ChatID, messageID)
		if payload.ReviewRequestID != "" {
			if payload.SettledCard {
				mustAgentTest(h.t, h.p.markCardSettled(h.ctx, payload.ReviewRequestID, payload.ChatID, messageID))
			}
			mustAgentTest(h.t, h.p.BindReviewMessage(h.ctx, payload.ReviewRequestID, payload.ChatID, messageID, payload.Text))
		}
		h.remember(messageID, payload.Text, payload.ReplyMarkup)
	case "EDIT_TELEGRAM_MESSAGE":
		var payload EditPayload
		mustAgentTest(h.t, json.Unmarshal(raw, &payload))
		mustAgentTest(h.t, h.p.RecordReviewCardText(h.ctx, payload.ChatID, payload.MessageID, payload.Text))
		h.remember(payload.MessageID, payload.Text, payload.ReplyMarkup)
	}
}

func (h *contractHarness) remember(messageID int64, text string, markup *InlineKeyboardMarkup) {
	h.texts[messageID] = text
	h.markups[messageID] = nil
	if markup != nil {
		for _, row := range markup.InlineKeyboard {
			for _, button := range row {
				h.markups[messageID] = append(h.markups[messageID], button.Text+"|"+button.CallbackData)
			}
		}
	}
}

func (h *contractHarness) card(chat int64) (int64, string, []string) {
	h.t.Helper()
	var messageID int64
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT COALESCE(telegram_message_id,0) FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2`, h.reviewID, chat).Scan(&messageID))
	return messageID, h.texts[messageID], h.markups[messageID]
}

func (h *contractHarness) current() (int64, string, []string) { return h.card(h.f.chatID) }

func (h *contractHarness) categoryID(name string) string {
	var id string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM category WHERE household_id=$1 AND name=$2`, h.f.householdID, name).Scan(&id))
	return id
}

func (h *contractHarness) tapOn(chat, messageID int64, text, data string) {
	h.t.Helper()
	update := callbackUpdate(chat, messageID, data)
	update.CallbackQuery.Message.Text = text
	raw, err := json.Marshal(update)
	mustAgentTest(h.t, err)
	source := h.f.seedSourceEvent(h.ctx, h.t, fmt.Sprintf("contract-tap-%d", nextMessageID()), raw)
	mustAgentTest(h.t, h.p.Process(h.ctx, source))
	h.deliver()
}

func (h *contractHarness) tap(data string) {
	h.t.Helper()
	messageID, text, _ := h.current()
	h.tapOn(h.f.chatID, messageID, text, data)
}

func (h *contractHarness) tapLabel(label string) {
	h.t.Helper()
	_, _, buttons := h.current()
	for _, button := range buttons {
		if strings.HasPrefix(button, label+"|") {
			h.tap(strings.TrimPrefix(button, label+"|"))
			return
		}
	}
	h.t.Fatalf("no %q button on the card: %v", label, buttons)
}

func (h *contractHarness) reply(text string) {
	h.t.Helper()
	messageID, _, _ := h.current()
	_, err := sendText(h.t, h.ctx, h.f, h.p, text, messageID)
	mustAgentTest(h.t, err)
	h.deliver()
}

func (h *contractHarness) plain(text string) {
	h.t.Helper()
	_, err := sendText(h.t, h.ctx, h.f, h.p, text, 0)
	mustAgentTest(h.t, err)
	h.deliver()
}

func (h *contractHarness) chat(text string) {
	h.t.Helper()
	h.audit()
	_, err := sendText(h.t, h.ctx, h.f, h.p, text, 0)
	mustAgentTest(h.t, err)
	h.absorb()
}

func (h *contractHarness) wantAssistantCalls(count int) {
	h.t.Helper()
	if h.picker.agentCalls != count {
		h.t.Fatalf("assistant calls = %d, want %d", h.picker.agentCalls, count)
	}
}

func (h *contractHarness) wantTurns(user, assistant string) {
	h.t.Helper()
	var gotUser, gotAssistant string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT COALESCE(max(message_text) FILTER (WHERE role='USER'),''),COALESCE(max(message_text) FILTER (WHERE role='ASSISTANT'),'') FROM telegram_conversation_turn WHERE household_id=$1 AND telegram_chat_id=$2 AND public_context_json->>'step_engine'='true' AND message_text ILIKE ANY(ARRAY[$3,$4])`, h.f.householdID, h.f.chatID, user, "%"+assistant+"%").Scan(&gotUser, &gotAssistant))
	if gotUser != user || !strings.Contains(gotAssistant, assistant) {
		h.t.Fatalf("conversation turns = %q / %q, want %q / %q", gotUser, gotAssistant, user, assistant)
	}
}

func (h *contractHarness) threadReply(text string) {
	h.t.Helper()
	messageID, _, _ := h.current()
	followUp := nextMessageID()
	h.absorb()
	seedBotMessage(h.t, h.ctx, h.f, "Catatan tambahan.", messageID, followUp)
	h.absorb()
	_, err := sendText(h.t, h.ctx, h.f, h.p, text, followUp)
	mustAgentTest(h.t, err)
	h.deliver()
}

func (h *contractHarness) fastLane(text string) bool {
	h.t.Helper()
	messageID, _, _ := h.current()
	source := seedText(h.t, h.ctx, h.f, text, nextMessageID(), messageID)
	handedOff, err := h.p.ProcessCardAnswer(h.ctx, source)
	mustAgentTest(h.t, err)
	h.deliver()
	return handedOff
}

func (h *contractHarness) wantCard(parts ...string) {
	h.t.Helper()
	_, text, _ := h.current()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			h.t.Fatalf("card lacks %q:\n%s", part, text)
		}
	}
}

func (h *contractHarness) wantButtons(labels ...string) {
	h.t.Helper()
	_, text, buttons := h.current()
	for _, label := range labels {
		found := false
		for _, button := range buttons {
			found = found || strings.HasPrefix(button, label+"|")
		}
		if !found {
			h.t.Fatalf("card lacks the %q button: %v\n%s", label, buttons, text)
		}
	}
}

func (h *contractHarness) wantNoButtons() {
	h.t.Helper()
	if _, text, buttons := h.current(); len(buttons) != 0 {
		h.t.Fatalf("a finished card still has buttons %v:\n%s", buttons, text)
	}
}

func (h *contractHarness) wantTransaction(status, merchant, category string) {
	h.t.Helper()
	var gotStatus, gotMerchant, gotCategory string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT t.status,COALESCE(m.normalized_name,''),COALESCE(c.name,'') FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id LEFT JOIN category c ON c.id=t.category_id WHERE t.id=$1`, h.txID).Scan(&gotStatus, &gotMerchant, &gotCategory))
	if gotStatus != status || gotMerchant != merchant || gotCategory != category {
		h.t.Fatalf("transaction = %s / %q / %q, want %s / %q / %q", gotStatus, gotMerchant, gotCategory, status, merchant, category)
	}
}

func (h *contractHarness) wantAlias(want bool) {
	h.t.Helper()
	var count int
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT count(*) FROM merchant_alias WHERE household_id=$1 AND auto_apply AND created_from_user_confirmation`, h.f.householdID).Scan(&count))
	if (count > 0) != want {
		h.t.Fatalf("remembered merchant rules = %d, want present=%v", count, want)
	}
}

func (h *contractHarness) wantItem(status string) {
	h.t.Helper()
	var got string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT ri.status FROM review_item ri JOIN review_request r ON r.review_item_id=ri.id WHERE r.id=$1`, h.reviewID).Scan(&got))
	if got != status {
		h.t.Fatalf("review item = %s, want %s", got, status)
	}
}

func (h *contractHarness) wantModelCalls(count int) {
	h.t.Helper()
	if len(h.picker.tools) != count {
		h.t.Fatalf("model calls = %d, want %d", len(h.picker.tools), count)
	}
}

func (h *contractHarness) wantAnswered(sources ...string) {
	h.t.Helper()
	wantInteraction(h.t, h.ctx, h.f, interactionAnswered, sources...)
}

func (h *contractHarness) wantJobs(sql string, want int, args ...any) {
	h.t.Helper()
	var got int
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, sql, args...).Scan(&got))
	if got != want {
		h.t.Fatalf("%s = %d, want %d", sql, got, want)
	}
}

var contractDone = "✅ Tersimpan: Warung Contoh · Makan · Rp25.000"

func TestInteractionEngineContract(t *testing.T) {
	rows := []struct {
		name    string
		kind    string
		options []contractOption
		run     func(*contractHarness)
	}{
		{"merchant typed as a reply, category tap, remember once", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			h.wantCard("Ini bayar ke mana?")
			h.wantButtons("Beli aset", "Abaikan")
			h.reply("  Warung   Contoh ")
			h.wantCard("✅ Warung Contoh disimpan. Masuk kategori apa?", "Nominal: Rp25.000")
			h.wantButtons("Makan", "Transportasi", "Abaikan")
			h.tapLabel("Makan")
			h.wantCard("✅ Dicatat: Makan.", "Pakai Makan juga untuk transaksi berikutnya dari Warung Contoh?")
			h.wantButtons("Ya, ingat", "Sekali ini")
			h.tapLabel("Sekali ini")
			h.wantCard(contractDone, "hanya dipakai untuk transaksi ini")
			h.wantNoButtons()
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantAlias(false)
			h.wantModelCalls(0)
			h.wantAnswered("REPLY", "TAP", "TAP")
		}},
		{"a typed answer and its card text are kept in the conversation memory", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			h.reply("Warung Contoh")
			h.wantTurns("Warung Contoh", "Masuk kategori apa?")
		}},
		{"a plain question while a merchant card waits goes to the assistant", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantTransaction("NEEDS_REVIEW", "", "")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1`, 1, h.reviewID)
		}},
		{"a plain question while a category card waits goes to the assistant even if the check says NONE", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.picker.script = []string{"NONE"}
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantTransaction("NEEDS_REVIEW", "Toko Contoh", "")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1`, 1, h.reviewID)
		}},
		{"a plain yes on the remember step answers the card", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tapLabel("Makan")
			h.plain("ya")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp25.000", "Diingat untuk Toko Contoh")
			h.wantAlias(true)
			h.wantAssistantCalls(0)
			h.wantAnswered("TAP", "PLAIN")
		}},
		{"a thread reply to the remember step answers the card", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tapLabel("Makan")
			h.threadReply("tidak")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp25.000", "hanya dipakai untuk transaksi ini")
			h.wantAlias(false)
			h.wantAssistantCalls(0)
			h.wantAnswered("TAP", "THREAD_REPLY")
		}},
		{"merchant typed as a plain message to the single waiting card", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			h.plain("Warung Contoh")
			h.wantCard("✅ Warung Contoh disimpan. Masuk kategori apa?")
			h.wantButtons("Makan")
			h.wantModelCalls(0)
			h.wantAnswered("PLAIN")
		}},
		{"merchant typed as a thread reply", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			h.threadReply("Warung Contoh")
			h.wantCard("✅ Warung Contoh disimpan. Masuk kategori apa?")
			h.wantModelCalls(0)
			h.wantAnswered("THREAD_REPLY")
		}},
		{"unusable merchant text is asked again with the same buttons", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			h.reply("???")
			h.wantCard("Maaf, aku belum menangkap itu. Ini bayar ke mana?")
			h.wantButtons("Beli aset", "Abaikan")
			h.wantTransaction("NEEDS_REVIEW", "", "")
		}},
		{"remembered merchant typed finishes with the saved category", "UNKNOWN_MERCHANT", []contractOption{withRememberedMerchant()}, func(h *contractHarness) {
			h.reply("warung contoh")
			h.wantCard(contractDone)
			h.wantNoButtons()
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
		}},
		{"category tap straight on an ambiguous category card, remember yes", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.wantCard("Masuk kategori apa?", "Merchant: Toko Contoh")
			h.tapLabel("Makan")
			h.wantCard("Pakai Makan juga untuk transaksi berikutnya dari Toko Contoh?")
			h.tapLabel("Ya, ingat")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp25.000", "Diingat untuk Toko Contoh")
			h.wantNoButtons()
			h.wantAlias(true)
			h.wantTransaction("CONFIRMED", "Toko Contoh", "Makan")
		}},
		{"category typed with its exact name", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.reply("  MAKAN ")
			h.wantCard("Pakai Makan juga untuk transaksi berikutnya dari Toko Contoh?")
			h.wantButtons("Ya, ingat", "Sekali ini")
			h.reply("ya")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp25.000", "Diingat untuk Toko Contoh")
			h.wantAlias(true)
			h.wantModelCalls(0)
		}},
		{"unclear category text is resolved by the narrow check", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.picker.script = []string{"transportasi"}
			h.reply("buat ongkos pulang")
			h.wantCard("✅ Dicatat: Transportasi.")
			h.wantTransaction("CONFIRMED", "Toko Contoh", "Transportasi")
			h.wantModelCalls(1)
			schema, _ := json.Marshal(h.picker.tools[0][0].Parameters)
			input, _ := json.Marshal(h.picker.inputs[0])
			if !strings.Contains(string(schema), `"NONE"`) || !strings.Contains(string(schema), `"transportasi"`) || len(h.picker.tools[0]) != 1 {
				t.Fatalf("the check offers more than the step's options: %s", schema)
			}
			if regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-`).Match(input) {
				t.Fatalf("a canonical id reached the model: %s", input)
			}
		}},
		{"unclear category text answered NONE asks the same step again", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.picker.script = []string{"NONE"}
			h.reply("entahlah")
			h.wantCard("Maaf, aku belum menangkap itu. Masuk kategori apa?")
			h.wantButtons("Makan", "Abaikan")
			h.wantTransaction("NEEDS_REVIEW", "Toko Contoh", "")
			h.wantModelCalls(1)
		}},
		{"unavailable model asks the same step again", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.picker.err = fmt.Errorf("gateway unavailable")
			h.reply("entahlah")
			h.wantCard("Maaf, aku belum menangkap itu. Masuk kategori apa?")
			h.wantButtons("Makan", "Abaikan")
			h.wantTransaction("NEEDS_REVIEW", "Toko Contoh", "")
		}},
		{"paging edits the card in place and a page two pick lands", "AMBIGUOUS_CATEGORY", []contractOption{withExtraCategories(12)}, func(h *contractHarness) {
			before, _, _ := h.current()
			h.tapLabel("Berikutnya")
			after, _, _ := h.current()
			if before != after {
				t.Fatalf("paging moved the card from message %d to %d", before, after)
			}
			h.wantCard("(halaman 2)")
			h.tapLabel("Kategori 12")
			h.wantCard("✅ Dicatat: Kategori 12.")
			h.wantTransaction("CONFIRMED", "Toko Contoh", "Kategori 12")
		}},
		{"abaikan finishes the card without recording", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: Toko Contoh · Rp25.000. Tidak dicatat.")
			h.wantNoButtons()
			h.wantTransaction("VOIDED", "Toko Contoh", "")
			h.wantItem("RESOLVED")
		}},
		{"a button label typed as text is the tap", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.reply("Abaikan")
			h.wantCard("🗑️ Diabaikan")
			h.wantTransaction("VOIDED", "Toko Contoh", "")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"answering on one phone closes the other member's copy", "AMBIGUOUS_CATEGORY", []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			if messageB == 0 {
				t.Fatal("the second member never received the card")
			}
			h.tapLabel("Makan")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
		{"a typed merchant moves the other member's copy to the category step", "UNKNOWN_MERCHANT", []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			h.reply("Warung Contoh")
			if text := h.texts[messageB]; !strings.Contains(text, "Masuk kategori apa?") {
				t.Fatalf("the other member's copy still shows the old step: %q", text)
			}
		}},
		{"a card answer through the fast lane lands on the engine", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			if h.fastLane("Warung Contoh") {
				t.Fatal("the fast lane handed an engine answer to the assistant")
			}
			h.wantCard("✅ Warung Contoh disimpan. Masuk kategori apa?")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='PROCESS_TELEGRAM_TEXT' AND household_id=$1`, 0, h.f.householdID)
			h.wantModelCalls(0)
			h.wantAnswered("REPLY")
		}},
		{"a fast lane answer that does not land is handed to the assistant once", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tapLabel("Makan")
			h.tapLabel("Sekali ini")
			if !h.fastLane("terima kasih") || !h.fastLane("terima kasih") {
				t.Fatal("a reply to a finished card stayed in the fast lane")
			}
			h.wantJobs(`SELECT count(*) FROM job WHERE type='PROCESS_TELEGRAM_TEXT' AND household_id=$1`, 2, h.f.householdID)
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := newContractHarness(t, row.kind, row.options...)
			row.run(h)
			h.deliver()
		})
	}
}
