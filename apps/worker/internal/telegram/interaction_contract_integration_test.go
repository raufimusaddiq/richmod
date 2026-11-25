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
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
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
	t             *testing.T
	ctx           context.Context
	f             agentIntegrationFixture
	p             *Processor
	picker        *scriptedPicker
	kind          string
	txType        string
	reconcile     bool
	mergeSource   string
	chatB         int64
	reviewID      string
	txID          string
	itemID        string
	imageID       string
	caseID        string
	primary       bool
	otherPrimary  bool
	largeLeftover bool
	unlinked      bool
	knownAmount   bool
	missingFacts  []string
	checked       map[string]bool
	markups       map[int64][]string
	texts         map[int64]string
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

func withTransactionType(kind string) contractOption {
	return func(h *contractHarness) { h.txType = kind }
}

func withAssetAccounts(names ...string) contractOption {
	return func(h *contractHarness) {
		for _, name := range names {
			h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,$2,'ASSET','GOLD','OTHER')`, h.f.householdID, name)
		}
	}
}

func withAssetAccountAt(name, institution string) contractOption {
	return func(h *contractHarness) {
		h.exec(`INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,$2,$3,'ASSET','GOLD','OTHER')`, h.f.householdID, name, institution)
	}
}

func withInvestmentAccounts(names ...string) contractOption {
	return func(h *contractHarness) {
		for _, name := range names {
			h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,$2,'ASSET','MUTUAL_FUND','INVESTMENT')`, h.f.householdID, name)
		}
	}
}

func withMergeCandidate() contractOption {
	return func(h *contractHarness) { h.reconcile = true }
}

func withActiveMergeTarget() contractOption {
	return func(h *contractHarness) {
		var duplicate string
		h.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,voided_at) VALUES($1,'EXPENSE','VOIDED',25000,now(),now())`, h.f.householdID)
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id::text FROM transaction WHERE household_id=$1 AND status='VOIDED'`, h.f.householdID).Scan(&duplicate))
		h.mergeSource = duplicate
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
	h := &contractHarness{t: t, ctx: ctx, f: f, p: NewProcessor(f.pool, picker), picker: picker, kind: kind, txType: "EXPENSE",
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

func (h *contractHarness) openReconciliation() {
	h.t.Helper()
	var account, candidate, source, item string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening Contoh','BANK','FULL_LEDGER') RETURNING id`, h.f.householdID).Scan(&account))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at) VALUES($1,$2,'UNCLASSIFIED','NEEDS_REVIEW',500000,now()-interval '1 day') RETURNING id`, h.f.householdID, account).Scan(&candidate))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-xfer-%d", h.chatB), []byte("contract-xfer")).Scan(&source))
	h.exec(`INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,candidate_transaction_ids) VALUES($1,$2,$3,500000,now(),'ke tabungan','INTERNAL_TRANSFER',ARRAY[$4::uuid])`, h.f.householdID, source, account, candidate)
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","decisionClass":"EVIDENCE_GAP","missingFacts":["transfer_relationship"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}'::jsonb) RETURNING id`, h.f.householdID, source).Scan(&item))
	tx, err := h.f.pool.Begin(h.ctx)
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, ProjectReviewItem(h.ctx, tx, h.f.householdID, item, 0, "", h.f.chatID))
	mustAgentTest(h.t, tx.Commit(h.ctx))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM review_request WHERE review_item_id=$1`, item).Scan(&h.reviewID))
	h.txID = candidate
	h.deliver()
}

func (h *contractHarness) openReview() {
	h.t.Helper()
	switch {
	case h.kind == "PAYSLIP_CONFIRMATION" || h.kind == "MISSING_PAY_DATE":
		h.itemID, h.imageID = h.seedPayslip(fmt.Sprint(h.chatB))
		h.project(h.itemID)
		return
	case h.kind == "CYCLE_RESIDUAL_ALLOCATION":
		h.openCycle()
		return
	case h.kind == "UNKNOWN_BANK_TEMPLATE":
		h.openBank()
		return
	case h.kind == "MISSING_AMOUNT":
		h.openProposalAmount()
		return
	}
	if h.reconcile {
		h.openReconciliation()
		return
	}
	merchant := ""
	if h.kind == "AMBIGUOUS_CATEGORY" {
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Toko Contoh') RETURNING id`, h.f.householdID).Scan(&merchant))
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,created_by_user_id) VALUES($1,$4,'NEEDS_REVIEW',25000,'IDR',now(),NULLIF($2,'')::uuid,$3) RETURNING id`, h.f.householdID, merchant, h.f.userID, h.txType).Scan(&h.txID))
	if h.kind == "MISSING_TRANSACTION_DATE" {
		h.exec(`UPDATE transaction SET category_id=(SELECT id FROM category WHERE household_id=$1 AND slug='makan') WHERE id=$2`, h.f.householdID, h.txID)
	}
	if h.mergeSource != "" {
		h.exec(`INSERT INTO reconciliation_merge(household_id,source_transaction_id,target_transaction_id,status,created_by_user_id) VALUES($1,$2,$3,'ACTIVE',$4)`, h.f.householdID, h.mergeSource, h.txID, h.f.userID)
	}
	facts := "Nominal: Rp25.000"
	if merchant != "" {
		facts += "\nMerchant: Toko Contoh"
	}
	tx, err := h.f.pool.Begin(h.ctx)
	mustAgentTest(h.t, err)
	var preset []reviewdec.Decision
	if len(h.missingFacts) > 0 {
		decision, _ := reviewdec.Preset(h.kind, "transaction", h.txID)
		decision.MissingFacts = h.missingFacts
		preset = append(preset, decision)
	}
	mustAgentTest(h.t, EnqueueReviewRequest(h.ctx, tx, h.txID, h.kind, h.f.chatID, 0, facts, preset...))
	mustAgentTest(h.t, tx.Commit(h.ctx))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, h.txID).Scan(&h.reviewID))
	h.deliver()
}

func (h *contractHarness) chats() []int64 { return []int64{h.f.chatID, h.chatB} }

func (h *contractHarness) cardMessages() []int64 {
	rows, err := h.f.pool.Query(h.ctx, `SELECT telegram_message_id FROM review_request_recipient WHERE telegram_chat_id=ANY($1) AND telegram_message_id IS NOT NULL
		UNION SELECT telegram_message_id FROM review_request_message WHERE telegram_chat_id=ANY($1)`, h.chats())
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

func (h *contractHarness) replyTo(messageID int64, text string) {
	h.t.Helper()
	_, err := sendText(h.t, h.ctx, h.f, h.p, text, messageID)
	mustAgentTest(h.t, err)
	h.deliver()
}

func (h *contractHarness) wantNoButton(label string) {
	h.t.Helper()
	_, text, buttons := h.current()
	for _, button := range buttons {
		if strings.HasPrefix(button, label+"|") {
			h.t.Fatalf("card offers %q:\n%s", label, text)
		}
	}
}

func (h *contractHarness) wantType(typ, status string) {
	h.t.Helper()
	var gotType, gotStatus string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT type,status FROM transaction WHERE id=$1`, h.txID).Scan(&gotType, &gotStatus))
	if gotType != typ || gotStatus != status {
		h.t.Fatalf("transaction = %s / %s, want %s / %s", gotType, gotStatus, typ, status)
	}
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

func (h *contractHarness) wantItemActive() {
	h.t.Helper()
	var got string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT ri.status FROM review_item ri JOIN review_request r ON r.review_item_id=ri.id WHERE r.id=$1`, h.reviewID).Scan(&got))
	if got != "OPEN" && got != "PENDING_SEND" {
		h.t.Fatalf("review item = %s, want it still active", got)
	}
}

func (h *contractHarness) wantTransfer(typ, status, purpose, account string) {
	h.t.Helper()
	var gotType, gotStatus, gotPurpose, gotAccount, merchant string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT t.type,t.status,t.purpose,COALESCE(w.name,''),COALESCE(t.merchant_id::text,'') FROM transaction t LEFT JOIN wealth_account w ON w.id=t.related_wealth_account_id WHERE t.id=$1`, h.txID).Scan(&gotType, &gotStatus, &gotPurpose, &gotAccount, &merchant))
	if gotType != typ || gotStatus != status || gotPurpose != purpose || gotAccount != account || merchant != "" {
		h.t.Fatalf("transaction = %s / %s / %s / %q / merchant %q, want %s / %s / %s / %q / none", gotType, gotStatus, gotPurpose, gotAccount, merchant, typ, status, purpose, account)
	}
	var merchantAliases int
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT count(*) FROM merchant WHERE household_id=$1`, h.f.householdID).Scan(&merchantAliases))
	if merchantAliases != 0 {
		h.t.Fatalf("an account name was saved as a merchant")
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
		{"a question thread-replied to a merchant card reaches the assistant", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			followUp := nextMessageID()
			h.absorb()
			seedBotMessage(h.t, h.ctx, h.f, "Catatan tambahan.", messageID, followUp)
			h.absorb()
			_, err := sendText(h.t, h.ctx, h.f, h.p, "ini merchant apa maksudnya?", followUp)
			mustAgentTest(h.t, err)
			h.absorb()
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantTransaction("NEEDS_REVIEW", "", "")
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
		{"a stale navigation tap on an engine card is left to the legacy handler", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tap("review:edit")
			h.wantCard("Pilih detail yang ingin diubah:")
		}},
		{"a plain yes on the remember step answers the card", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tapLabel("Makan")
			h.plain("ya")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp25.000", "Diingat untuk Toko Contoh")
			h.wantAlias(true)
			h.wantAssistantCalls(0)
			h.wantAnswered("TAP", "PLAIN")
		}},
		{"a plain yes meant for a pending confirmation is not taken as the remember answer", "AMBIGUOUS_CATEGORY", nil, func(h *contractHarness) {
			h.tapLabel("Makan")
			var pendingTx string
			mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',9000,now()) RETURNING id`, h.f.householdID).Scan(&pendingTx))
			h.exec(`INSERT INTO telegram_pending_action(household_id,telegram_user_id,telegram_chat_id,transaction_id,proposed_transaction_at,status) VALUES($1,$2,$2,$3,now(),'PENDING')`, h.f.householdID, h.f.chatID, pendingTx)
			h.chat("ya")
			h.wantAlias(false)
			h.wantAnswered("TAP")
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
			h.reply("!!!")
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
			h.tap("review:catpage:9")
			if _, text, _ := h.current(); strings.Contains(text, "(halaman") {
				t.Fatalf("a page that fell back to the first page kept its page number:\n%s", text)
			}
			h.tapLabel("Berikutnya")
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

func TestInteractionEngineTransferContract(t *testing.T) {
	const transfer = "TRANSFER_CLASSIFICATION"
	unclassified := withTransactionType("UNCLASSIFIED")
	rows := []struct {
		name    string
		kind    string
		options []contractOption
		run     func(*contractHarness)
	}{
		{"outgoing: Pengeluaran then a category finishes the card", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.wantCard("Ini untuk apa?", "Nominal: Rp25.000")
			h.wantButtons("Pengeluaran", "Beli aset", "Ke rekening sendiri", "Rumah tangga", "Investasi", "Abaikan")
			h.tapLabel("Pengeluaran")
			h.wantCard("Masuk kategori apa?")
			h.wantButtons("Makan", "Kembali", "Abaikan")
			h.tapLabel("Makan")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000")
			h.wantNoButtons()
			h.wantType("EXPENSE", "CONFIRMED")
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
		}},
		{"outgoing: a category typed as a reply finishes the card", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.tapLabel("Pengeluaran")
			h.reply("makan")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000")
			h.wantType("EXPENSE", "CONFIRMED")
		}},
		{"outgoing: Ke rekening sendiri", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.tapLabel("Ke rekening sendiri")
			h.wantCard("✅ Tersimpan: Antar rekening sendiri · Rp25.000")
			h.wantNoButtons()
			h.wantTransfer("TRANSFER", "CONFIRMED", "INTERNAL_TRANSFER", "")
			h.wantItem("RESOLVED")
		}},
		{"outgoing: Rumah tangga", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.tapLabel("Rumah tangga")
			h.wantCard("✅ Tersimpan: Antar anggota keluarga · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "INTERNAL_TRANSFER", "")
		}},
		{"outgoing: Beli aset then a tapped account", transfer, []contractOption{unclassified, withAssetAccounts("Emas", "Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.wantCard("Beli aset ke akun mana?", "Nominal: Rp25.000")
			h.wantButtons("Emas", "Reksa Contoh", "Kembali", "Abaikan")
			h.tapLabel("Emas")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantNoButtons()
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
		}},
		{"outgoing: Beli aset then the account's institution as a reply", transfer, []contractOption{unclassified, withAssetAccountAt("Emas", "Antam"), withAssetAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.reply("  ANTAM ")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantModelCalls(0)
			h.wantAnswered("TAP", "REPLY")
		}},
		{"outgoing: Beli aset then the account name typed as a thread reply", transfer, []contractOption{unclassified, withAssetAccountAt("Emas", "Antam")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.threadReply("Antam")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantAnswered("TAP", "THREAD_REPLY")
		}},
		{"outgoing: a reply to an older copy of the asset card still answers it", transfer, []contractOption{unclassified, withAssetAccountAt("Emas", "Antam"), withAssetAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			older, _, _ := h.current()
			h.reply("akun yang mana ya")
			if newer, _, _ := h.current(); newer == older {
				t.Fatal("the re-asked card was not sent as a fresh message")
			}
			h.wantCard("Beli aset ke akun mana?")
			h.replyTo(older, "Antam")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantAssistantCalls(0)
		}},
		{"outgoing: an unknown account name typed as a reply shows the buttons again", transfer, []contractOption{unclassified, withAssetAccounts("Emas", "Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.reply("Rekening Tidak Ada")
			h.wantCard("Aku belum menemukan satu akun yang cocok dengan “Rekening Tidak Ada”.", "Beli aset ke akun mana?")
			h.wantButtons("Emas", "Reksa Contoh", "Kembali")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
			h.wantItemActive()
			h.wantAssistantCalls(0)
		}},
		{"outgoing: an unknown account name typed plainly goes to the assistant", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"outgoing: a stale Beli aset tap the first step does not offer shows the step again", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.exec(`UPDATE review_item SET decision=jsonb_set(decision,'{allowedActions}','["CLASSIFY_TRANSFER","EXPENSE","IGNORE"]'::jsonb) WHERE id=(SELECT review_item_id FROM review_request WHERE id=$1)`, h.reviewID)
			h.tap("review:asset")
			h.wantCard("Pilihan itu tidak berlaku untuk transaksi ini.", "Ini untuk apa?")
			h.wantNoButton("Kembali")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
			h.wantItemActive()
		}},
		{"outgoing: Kembali returns to the first step", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			before, _, _ := h.current()
			h.tapLabel("Beli aset")
			h.tapLabel("Kembali")
			after, _, _ := h.current()
			if before != after {
				t.Fatalf("Kembali moved the card from message %d to %d", before, after)
			}
			h.wantCard("Ini untuk apa?")
			h.wantButtons("Pengeluaran", "Beli aset", "Investasi")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"outgoing: Investasi then a tapped investment account", transfer, []contractOption{unclassified, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Investasi")
			h.wantCard("Investasi ke akun mana?")
			h.wantButtons("Reksa Contoh", "Kembali", "Abaikan")
			h.tapLabel("Reksa Contoh")
			h.wantCard("✅ Tersimpan: Investasi · Reksa Contoh · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "INVESTMENT_CONTRIBUTION", "Reksa Contoh")
			h.wantItem("RESOLVED")
		}},
		{"outgoing: Investasi with no active investment account says so and can go back", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.tapLabel("Investasi")
			h.wantCard("Belum ada akun investasi yang aktif")
			h.tapLabel("Kembali")
			h.wantCard("Ini untuk apa?")
		}},
		{"outgoing: Abaikan finishes without recording", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: Rp25.000. Tidak dicatat.")
			h.wantNoButtons()
			h.wantType("UNCLASSIFIED", "VOIDED")
			h.wantItem("RESOLVED")
		}},
		{"outgoing: Abaikan is refused while the row is a merge target", transfer, []contractOption{unclassified, withActiveMergeTarget()}, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("Transaksi ini masih jadi tujuan penggabungan")
			h.wantButtons("Pengeluaran", "Abaikan")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
			h.wantItemActive()
		}},
		{"outgoing: typed text that is not an answer asks the same step again", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.reply("entahlah")
			h.wantCard("Maaf, aku belum menangkap itu. Ini untuk apa?")
			h.wantButtons("Pengeluaran", "Beli aset")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"outgoing: a button label typed as text is the tap", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.reply("Rumah tangga")
			h.wantCard("✅ Tersimpan: Antar anggota keluarga · Rp25.000")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"expense row: Beli aset is offered and Pengeluaran asks for a category", transfer, nil, func(h *contractHarness) {
			h.wantButtons("Pengeluaran", "Beli aset", "Abaikan")
			h.wantNoButton("Ke rekening sendiri")
			h.tapLabel("Pengeluaran")
			h.tapLabel("Transportasi")
			h.wantCard("✅ Tersimpan: Transportasi · Rp25.000")
			h.wantTransaction("CONFIRMED", "", "Transportasi")
		}},
		{"merge: an existing transfer candidate is merged from the first step", transfer, []contractOption{withMergeCandidate()}, func(h *contractHarness) {
			h.wantCard("Ini untuk apa?", "Nominal: Rp500.000")
			h.wantButtons("Catat transfer baru", "Abaikan")
			h.tap("review:xfer:merge:0")
			h.wantCard("✅ Tersimpan: Digabung dengan catatan yang ada · Rp500.000")
			h.wantNoButtons()
			h.wantType("TRANSFER", "CONFIRMED")
			h.wantItem("RESOLVED")
		}},
		{"merge: Catat transfer baru records the transfer", transfer, []contractOption{withMergeCandidate()}, func(h *contractHarness) {
			h.tapLabel("Catat transfer baru")
			h.wantCard("✅ Tersimpan: Transfer baru · Rp500.000")
			h.wantItem("RESOLVED")
		}},
		{"merge: Abaikan dismisses the case", transfer, []contractOption{withMergeCandidate()}, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: Rp500.000. Tidak dicatat.")
			h.wantItem("RESOLVED")
		}},
		{"incoming: Pemasukan", transfer, []contractOption{withTransactionType("INCOME")}, func(h *contractHarness) {
			h.wantButtons("Pemasukan", "Ke rekening sendiri", "Rumah tangga", "Abaikan")
			h.wantNoButton("Pengeluaran")
			h.wantNoButton("Beli aset")
			h.tapLabel("Pemasukan")
			h.wantCard("✅ Tersimpan: Pemasukan · Rp25.000")
			h.wantNoButtons()
			h.wantType("INCOME", "CONFIRMED")
			h.wantItem("RESOLVED")
		}},
		{"incoming: Ke rekening sendiri", transfer, []contractOption{withTransactionType("INCOME")}, func(h *contractHarness) {
			h.tapLabel("Ke rekening sendiri")
			h.wantCard("✅ Tersimpan: Antar rekening sendiri · Rp25.000")
			h.wantType("TRANSFER", "CONFIRMED")
		}},
		{"incoming: Rumah tangga typed as a reply", transfer, []contractOption{withTransactionType("INCOME")}, func(h *contractHarness) {
			h.reply("rumah tangga")
			h.wantCard("✅ Tersimpan: Antar anggota keluarga · Rp25.000")
			h.wantType("TRANSFER", "CONFIRMED")
		}},
		{"income check: Benar confirms the income", "AMBIGUOUS_CATEGORY", []contractOption{withTransactionType("INCOME")}, func(h *contractHarness) {
			h.wantCard("Pemasukan ini perlu dicek.")
			h.wantButtons("Benar", "Ubah", "Abaikan")
			h.tapLabel("Benar")
			h.wantCard("✅ Tersimpan: Pemasukan · Rp25.000")
			h.wantType("INCOME", "CONFIRMED")
		}},
		{"income check: Ubah then Rumah tangga", "AMBIGUOUS_CATEGORY", []contractOption{withTransactionType("INCOME")}, func(h *contractHarness) {
			h.tapLabel("Ubah")
			h.wantCard("Ini untuk apa?")
			h.wantButtons("Pemasukan", "Ke rekening sendiri", "Rumah tangga")
			h.tapLabel("Rumah tangga")
			h.wantCard("✅ Tersimpan: Antar anggota keluarga · Rp25.000")
			h.wantType("TRANSFER", "CONFIRMED")
		}},
		{"a merchant card: Beli aset goes through the engine to an account", "UNKNOWN_MERCHANT", []contractOption{withAssetAccounts("Emas", "Reksa Contoh")}, func(h *contractHarness) {
			h.wantButtons("Beli aset", "Abaikan")
			h.tapLabel("Beli aset")
			h.wantCard("Beli aset ke akun mana?", "Nominal: Rp25.000")
			h.wantButtons("Emas", "Reksa Contoh", "Kembali")
			h.tapLabel("Emas")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
		}},
		{"a merchant card: Kembali from the account step returns to the merchant question", "UNKNOWN_MERCHANT", []contractOption{withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.tapLabel("Kembali")
			h.wantCard("Ini bayar ke mana?")
			h.reply("Warung Contoh")
			h.wantCard("✅ Warung Contoh disimpan. Masuk kategori apa?")
		}},
		{"a category card: Beli aset typed as the exact account name", "AMBIGUOUS_CATEGORY", []contractOption{withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.reply("Emas")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantType("TRANSFER", "CONFIRMED")
		}},
		{"asset accounts are paged in place and a page two pick lands", transfer, []contractOption{unclassified, withAssetAccounts("Akun 01", "Akun 02", "Akun 03", "Akun 04", "Akun 05", "Akun 06", "Akun 07", "Akun 08", "Akun 09", "Akun 10")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			before, _, _ := h.current()
			h.wantButtons("Akun 08", "Berikutnya")
			h.wantNoButton("Akun 09")
			h.tapLabel("Berikutnya")
			after, _, _ := h.current()
			if before != after {
				t.Fatalf("paging moved the card from message %d to %d", before, after)
			}
			h.wantCard("(halaman 2)")
			h.wantButtons("Akun 09", "Akun 10", "Sebelumnya", "Kembali")
			h.tapLabel("Akun 10")
			h.wantCard("✅ Tersimpan: Beli aset · Akun 10 · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Akun 10")
		}},
		{"an account on page two typed by name lands", transfer, []contractOption{unclassified, withAssetAccounts("Akun 01", "Akun 02", "Akun 03", "Akun 04", "Akun 05", "Akun 06", "Akun 07", "Akun 08", "Akun 09", "Akun 10")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.reply("akun 10")
			h.wantCard("✅ Tersimpan: Beli aset · Akun 10 · Rp25.000")
		}},
		{"an account on page two typed plainly finishes the card", transfer, []contractOption{unclassified, withAssetAccounts("Akun 01", "Akun 02", "Akun 03", "Akun 04", "Akun 05", "Akun 06", "Akun 07", "Akun 08", "Akun 09", "Akun 10 dengan nama yang sangat panjang sekali")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.wantNoButton("Akun 10 dengan nama yang sangat panjang sekali")
			h.plain("akun 10 dengan nama yang sangat panjang sekali")
			h.wantCard("✅ Tersimpan: Beli aset · Akun 10 dengan nama yang sangat panjang sekali · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Akun 10 dengan nama yang sangat panjang sekali")
			h.wantAssistantCalls(0)
		}},
		{"an institution typed plainly is not an exact name and goes to the assistant", transfer, []contractOption{unclassified, withAssetAccountAt("Emas", "Antam")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.chat("antam")
			h.wantAssistantCalls(1)
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"an account name typed plainly while two account steps wait asks which card and saves nothing", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			var second string
			mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,created_by_user_id) VALUES($1,'UNCLASSIFIED','NEEDS_REVIEW',40000,'IDR',now(),$2) RETURNING id`, h.f.householdID, h.f.userID).Scan(&second))
			tx, err := h.f.pool.Begin(h.ctx)
			mustAgentTest(h.t, err)
			mustAgentTest(h.t, EnqueueReviewRequest(h.ctx, tx, second, transfer, h.f.chatID, 0, "Nominal: Rp40.000"))
			mustAgentTest(h.t, tx.Commit(h.ctx))
			h.deliver()
			var secondReview string
			var secondMessage int64
			mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT r.id::text,rr.telegram_message_id FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.transaction_id=$1 AND rr.telegram_chat_id=$2`, second, h.f.chatID).Scan(&secondReview, &secondMessage))
			h.tapOn(h.f.chatID, secondMessage, h.texts[secondMessage], "review:asset")
			h.chat("Emas")
			h.wantAssistantCalls(0)
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
			var status string
			mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT status FROM transaction WHERE id=$1`, second).Scan(&status))
			if status != "NEEDS_REVIEW" {
				t.Fatalf("the second card was answered: %s", status)
			}
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text' LIKE 'Ada beberapa kartu%'`, 1)
		}},
		{"investment accounts are paged by position", transfer, []contractOption{unclassified, withInvestmentAccounts("Dana 01", "Dana 02", "Dana 03", "Dana 04", "Dana 05", "Dana 06", "Dana 07", "Dana 08", "Dana 09")}, func(h *contractHarness) {
			h.tapLabel("Investasi")
			h.tapLabel("Berikutnya")
			h.wantCard("(halaman 2)")
			h.tapLabel("Dana 09")
			h.wantCard("✅ Tersimpan: Investasi · Dana 09 · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "INVESTMENT_CONTRIBUTION", "Dana 09")
		}},
		{"a savings transfer with a stale account points to the app instead of a dead end", transfer, []contractOption{withTransactionType("TRANSFER")}, func(h *contractHarness) {
			h.wantCard("Ini untuk apa?", "Kotak Tinjauan")
			h.wantButtons("Abaikan")
			h.wantNoButton("Beli aset")
		}},
		{"Investasi with one linked account finishes without a question", transfer, []contractOption{unclassified, withInvestmentAccounts("Reksa Contoh", "Dana Contoh")}, func(h *contractHarness) {
			var account string
			mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM wealth_account WHERE household_id=$1 AND name='Dana Contoh'`, h.f.householdID).Scan(&account))
			h.exec(`UPDATE transaction SET counterparty_name='RDN contoh' WHERE id=$1`, h.txID)
			h.exec(`INSERT INTO known_account(household_id,user_id,institution,display_name,match_hint,relationship,wealth_account_id) VALUES($1,$2,'Contoh','Rekening investasi','rdn contoh','INVESTMENT_ACCOUNT',$3)`, h.f.householdID, h.f.userID, account)
			h.tapLabel("Investasi")
			h.wantCard("✅ Tersimpan: Investasi · Dana Contoh · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "INVESTMENT_CONTRIBUTION", "Dana Contoh")
		}},
		{"an account deactivated after the buttons were shown is refused", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.exec(`UPDATE wealth_account SET active=false WHERE household_id=$1`, h.f.householdID)
			h.tapLabel("Emas")
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Belum ada akun aset yang aktif")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"a typed name that fits two accounts asks again instead of guessing", transfer, []contractOption{unclassified, withAssetAccountAt("Emas A", "Antam"), withAssetAccountAt("Emas B", "Antam")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.reply("antam")
			h.wantCard("Aku belum menemukan satu akun yang cocok", "Beli aset ke akun mana?")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"answering on one phone updates then closes the other member's copy", transfer, []contractOption{unclassified, withAssetAccounts("Emas"), withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			if messageB == 0 {
				t.Fatal("the second member never received the card")
			}
			h.tapLabel("Beli aset")
			if text := h.texts[messageB]; !strings.Contains(text, "Beli aset ke akun mana?") {
				t.Fatalf("the other member's copy still shows the old step: %q", text)
			}
			h.tapLabel("Emas")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
		{"a button from an older copy never records a different account", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			_, _, buttons := h.current()
			var old string
			for _, button := range buttons {
				if strings.HasPrefix(button, "Emas|") {
					old = strings.TrimPrefix(button, "Emas|")
				}
			}
			h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Aaa Lain','ASSET','GOLD','OTHER')`, h.f.householdID)
			h.tap("review:acctpage:0")
			h.wantButtons("Aaa Lain", "Emas")
			h.tap(old)
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Beli aset ke akun mana?")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
		}},
		{"an account typed by name after the list changed still lands", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Aaa Lain','ASSET','GOLD','OTHER')`, h.f.householdID)
			h.reply("Emas")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
		}},
		{"a stale account tap is answered with the step, not guessed", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.tapLabel("Beli aset")
			h.tap("review:acct:7")
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Beli aset ke akun mana?")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
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

const (
	payslipHeldDecision     = `{"version":1,"reasonCode":"PAYSLIP_CONFIRMATION","decisionClass":"HUMAN_POLICY_CHOICE","interactionMode":"POLICY_CHOICE","knownFacts":{},"missingFacts":["salary_classification"],"allowedActions":["HOUSEHOLD_POLICY","ORDINARY_INCOME","IGNORE"],"whyNotAutoConfirm":"the payslip's gross pay, allowances and deductions do not add up to net pay","decisionSource":"DETERMINISTIC"}`
	payslipFirstDecision    = `{"version":1,"reasonCode":"MISSING_PAY_DATE","decisionClass":"HUMAN_POLICY_CHOICE","interactionMode":"POLICY_CHOICE","knownFacts":{},"missingFacts":["transaction_at","salary_classification"],"allowedActions":["SET_PAY_DATE","PRIMARY_SALARY","ORDINARY_INCOME","IGNORE"],"whyNotAutoConfirm":"pay date is absent and the first salary source requires household classification","decisionSource":"DETERMINISTIC"}`
	payslipDateOnlyDecision = `{"version":1,"reasonCode":"MISSING_PAY_DATE","decisionClass":"EVIDENCE_GAP","interactionMode":"SINGLE_FIELD","knownFacts":{},"missingFacts":["transaction_at"],"allowedActions":["SET_PAY_DATE","IGNORE"],"whyNotAutoConfirm":"the pay date is not present in the evidence","decisionSource":"DETERMINISTIC"}`
	cycleDecision           = `{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","decisionClass":"HUMAN_POLICY_CHOICE","missingFacts":["residual_allocation"],"allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"],"interactionMode":"POLICY_CHOICE"}`
	cycleResidualFixture    = "1200000"
	payslipDoneSalaryPrefix = "✅ Gaji tercatat · Rp8.500.000. Siklus baru mulai "
)

func withLargeLeftover() contractOption {
	return func(h *contractHarness) { h.largeLeftover = true }
}

func withOtherPrimaryEmployer() contractOption {
	return func(h *contractHarness) { h.otherPrimary = true }
}

func withPrimarySalary() contractOption {
	return func(h *contractHarness) { h.primary = true }
}

func withSavingsAccounts(names ...string) contractOption {
	return func(h *contractHarness) {
		for _, name := range names {
			h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,$2,'ASSET','DEPOSIT','SAVINGS')`, h.f.householdID, name)
		}
	}
}

func (h *contractHarness) seedPayslip(label string) (itemID, imageID string) {
	h.t.Helper()
	stamp := testsupport.UniqueStamp()
	var attachment, document, proposal string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-payslip-%s-%d", label, stamp), []byte(fmt.Sprint(stamp))).Scan(&imageID))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, h.f.householdID, []byte(fmt.Sprint(stamp)), fmt.Sprintf("contract/%d.png", stamp)).Scan(&attachment))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'PAYSLIP','NEEDS_REVIEW') RETURNING id`, h.f.householdID, imageID, attachment).Scan(&document))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,counterparty_raw,description,confidence,proposal_status,metadata_json) VALUES($1,$2,'INCOME',8500000,'IDR',now(),'PT Contoh','Penghasilan dari slip gaji',.99,'NEEDS_REVIEW',jsonb_build_object('period','2026-09','document_id',$3::uuid)) RETURNING id`, h.f.householdID, imageID, document).Scan(&proposal))
	decision, reviewType := payslipFirstDecision, "MISSING_PAY_DATE"
	switch {
	case h.kind == "PAYSLIP_CONFIRMATION":
		decision, reviewType = payslipHeldDecision, "PAYSLIP_CONFIRMATION"
		h.primary = true
	case h.primary:
		decision = payslipDateOnlyDecision
	}
	if h.primary {
		employer := "PT Contoh"
		if h.otherPrimary {
			employer = "PT Lain"
		}
		h.exec(`INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,lower($3),true) ON CONFLICT DO NOTHING`, h.f.householdID, h.f.userID, employer)
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,proposal_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,$4,$5,'OPEN',$6::jsonb) RETURNING id`, h.f.householdID, proposal, imageID, document, reviewType, decision).Scan(&itemID))
	return itemID, imageID
}

func (h *contractHarness) project(itemID string) {
	h.t.Helper()
	tx, err := h.f.pool.Begin(h.ctx)
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, ProjectReviewItem(h.ctx, tx, h.f.householdID, itemID, 0, "", h.f.chatID))
	mustAgentTest(h.t, tx.Commit(h.ctx))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM review_request WHERE review_item_id=$1`, itemID).Scan(&h.reviewID))
	h.deliver()
}

func (h *contractHarness) openCycle() {
	h.t.Helper()
	var salarySource, source, start, end, caseID string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'PT Contoh','pt contoh',true) RETURNING id`, h.f.householdID, h.f.userID).Scan(&salarySource))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-cycle-%d", h.chatB)).Scan(&source))
	salary := func(period string) string {
		var paid, event string
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1,'2025-01-01',now()) RETURNING id`, h.f.householdID).Scan(&paid))
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1,$4,'CONFIRMED',$5) RETURNING id`, salarySource, h.f.householdID, period, paid, source).Scan(&event))
		return event
	}
	income := cycleResidualFixture
	if h.largeLeftover {
		income = "150000000"
	}
	start, end = salary("2026-08-25"), salary("2026-09-25")
	h.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',$2::numeric,'2026-09-01T12:00:00+07:00',now())`, h.f.householdID, income)
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-08-25','2026-09-25',$4::numeric,0,0,$4::numeric) RETURNING id`, h.f.householdID, start, end, income).Scan(&caseID))
	h.caseID = caseID
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','PENDING_SEND',$3::jsonb) RETURNING id`, h.f.householdID, caseID, cycleDecision).Scan(&h.itemID))
	h.project(h.itemID)
}

func (h *contractHarness) wantButtonLabels(labels ...string) {
	h.t.Helper()
	_, text, buttons := h.current()
	got := make([]string, 0, len(buttons))
	for _, button := range buttons {
		got = append(got, strings.SplitN(button, "|", 2)[0])
	}
	if !slices.Equal(got, labels) {
		h.t.Fatalf("buttons = %q, want %q\n%s", got, labels, text)
	}
}

func (h *contractHarness) wantSalary(status, amount, monthDay string, count int) {
	h.t.Helper()
	var got int
	var gotStatus, gotAmount, gotDay string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT count(*),COALESCE(max(t.status),''),COALESCE(max(t.amount::text),''),COALESCE(max(to_char(t.transaction_at AT TIME ZONE 'Asia/Jakarta','MM-DD')),'')
		FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id AND e.evidence_type='PAYSLIP_IMAGE' AND e.source_event_id=$1`, h.imageID).Scan(&got, &gotStatus, &gotAmount, &gotDay))
	if got != count || (count > 0 && (gotStatus != status || gotAmount != amount || (monthDay != "" && gotDay != monthDay))) {
		h.t.Fatalf("payslip transactions = %d / %s / %s / %s, want %d / %s / %s / %s", got, gotStatus, gotAmount, gotDay, count, status, amount, monthDay)
	}
}

func (h *contractHarness) wantResolution(action string) {
	h.t.Helper()
	var status, got string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT status,COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, h.itemID).Scan(&status, &got))
	if status != "RESOLVED" || got != action {
		h.t.Fatalf("review item = %s / %s, want RESOLVED / %s", status, got, action)
	}
}

func (h *contractHarness) wantAllocations(want ...string) {
	h.t.Helper()
	rows, err := h.f.pool.Query(h.ctx, `SELECT w.name||' '||a.amount_idr::text FROM cycle_residual_allocation a JOIN wealth_account w ON w.id=a.wealth_account_id WHERE a.cycle_residual_case_id=$1 ORDER BY w.name`, h.caseID)
	mustAgentTest(h.t, err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var line string
		mustAgentTest(h.t, rows.Scan(&line))
		got = append(got, line)
	}
	if !slices.Equal(got, want) {
		h.t.Fatalf("allocations = %q, want %q", got, want)
	}
}

func (h *contractHarness) buttonData(label string) string {
	h.t.Helper()
	_, _, buttons := h.current()
	for _, button := range buttons {
		if strings.HasPrefix(button, label+"|") {
			return strings.TrimPrefix(button, label+"|")
		}
	}
	h.t.Fatalf("no %q button on the card: %v", label, buttons)
	return ""
}

func (h *contractHarness) question(replyTo int64, text string) {
	h.t.Helper()
	h.audit()
	_, err := sendText(h.t, h.ctx, h.f, h.p, text, replyTo)
	mustAgentTest(h.t, err)
	h.absorb()
}

func (h *contractHarness) staleTapOn(chat, messageID int64, text, data string) {
	h.t.Helper()
	h.audit()
	update := callbackUpdate(chat, messageID, data)
	update.CallbackQuery.Message.Text = text
	raw, err := json.Marshal(update)
	mustAgentTest(h.t, err)
	source := h.f.seedSourceEvent(h.ctx, h.t, fmt.Sprintf("contract-stale-%d", nextMessageID()), raw)
	mustAgentTest(h.t, h.p.Process(h.ctx, source))
	h.absorb()
}

func (h *contractHarness) wantNoBoundPrompt() {
	h.t.Helper()
	h.wantJobs(`SELECT count(*) FROM job WHERE type IN ('SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE') AND payload_json->>'text' LIKE '%Balas pesan ini%' AND (payload_json->>'chat_id')::bigint=ANY($1)`, 0, h.chats())
}

var contractMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func yesterdayLabel() (string, string) {
	day := time.Now().In(jakartaLocation()).AddDate(0, 0, -1)
	return day.Format("2") + " " + contractMonths[day.Month()-1], day.Format("01-02")
}

func runContractRows(t *testing.T, rows []contractRow) {
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := newContractHarness(t, row.kind, row.options...)
			row.run(h)
			h.deliver()
			h.wantNoBoundPrompt()
		})
	}
}

type contractRow struct {
	name    string
	kind    string
	options []contractOption
	run     func(*contractHarness)
}

func TestInteractionEngineSalaryContract(t *testing.T) {
	const held, first = "PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE"
	runContractRows(t, []contractRow{
		{"held payslip: facts, the held reason and the buttons in contract order", held, nil, func(h *contractHarness) {
			h.wantCard("Slip gaji ini dicatat sebagai apa?", "Pemberi kerja: PT Contoh", "Gaji bersih: Rp8.500.000", "Periode: Sep 2026",
				"Alasan: Gaji kotor, tunjangan, dan potongan di slip tidak cocok dengan gaji bersih.")
			h.wantButtonLabels("Catat sebagai gaji", "Pemasukan biasa", "Abaikan")
			h.tapLabel("Catat sebagai gaji")
			h.wantCard(payslipDoneSalaryPrefix)
			h.wantNoButtons()
			h.wantSalary("CONFIRMED", "8500000", "", 1)
			h.wantResolution("HOUSEHOLD_POLICY")
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"held payslip: a slip from an employer that is not the primary source records salary without claiming a cycle", held, []contractOption{withOtherPrimaryEmployer()}, func(h *contractHarness) {
			h.tapLabel("Catat sebagai gaji")
			h.wantCard("✅ Gaji tercatat · Rp8.500.000.")
			if _, text, _ := h.current(); strings.Contains(text, "Siklus baru") {
				t.Fatalf("a cycle was claimed for a non-primary source: %q", text)
			}
			h.wantSalary("CONFIRMED", "8500000", "", 1)
			h.wantJobs(`SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'household_id'=$1`, 0, h.f.householdID)
		}},
		{"held payslip: Pemasukan biasa records plain income", held, nil, func(h *contractHarness) {
			h.tapLabel("Pemasukan biasa")
			h.wantCard("✅ Pemasukan tercatat · Rp8.500.000")
			h.wantNoButtons()
			h.wantSalary("CONFIRMED", "8500000", "", 1)
			h.wantResolution("ORDINARY_INCOME")
		}},
		{"held payslip: Abaikan records nothing", held, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: PT Contoh · Rp8.500.000. Tidak dicatat.")
			h.wantNoButtons()
			h.wantSalary("", "", "", 0)
			h.wantResolution("IGNORE")
		}},
		{"held payslip: an exact label typed as a reply is the tap", held, nil, func(h *contractHarness) {
			h.reply("  PEMASUKAN biasa ")
			h.wantCard("✅ Pemasukan tercatat · Rp8.500.000")
			h.wantSalary("CONFIRMED", "8500000", "", 1)
			h.wantAnswered("LABEL_TEXT")
			h.wantModelCalls(0)
		}},
		{"held payslip: an exact label typed plainly is the tap", held, nil, func(h *contractHarness) {
			h.plain("catat sebagai gaji")
			h.wantCard(payslipDoneSalaryPrefix)
			h.wantAnswered("LABEL_TEXT")
		}},
		{"held payslip: an exact label thread-replied is the tap", held, nil, func(h *contractHarness) {
			h.threadReply("Pemasukan biasa")
			h.wantCard("✅ Pemasukan tercatat · Rp8.500.000")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"held payslip: ya and oke re-show the card and record nothing", held, nil, func(h *contractHarness) {
			h.reply("ya")
			h.wantCard("Maaf, aku belum menangkap itu.", "Slip gaji ini dicatat sebagai apa?")
			h.wantButtons("Catat sebagai gaji", "Pemasukan biasa", "Abaikan")
			h.plain("oke")
			h.wantCard("Maaf, aku belum menangkap itu.", "Slip gaji ini dicatat sebagai apa?")
			h.threadReply("ya")
			h.wantCard("Maaf, aku belum menangkap itu.", "Slip gaji ini dicatat sebagai apa?")
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
			h.wantAssistantCalls(0)
			h.wantModelCalls(0)
		}},
		{"held payslip: a question with a question mark goes to the assistant", held, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.question(messageID, "kenapa slip ini ditahan?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
		}},
		{"held payslip: untargeted chat is not swallowed", held, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantItemActive()
		}},
		{"held payslip: a tap from a finished card's older copy records nothing twice", held, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Pemasukan biasa")
			h.tapLabel("Pemasukan biasa")
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantSalary("CONFIRMED", "8500000", "", 1)
			h.wantResolution("ORDINARY_INCOME")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
		{"first payslip without a date: choice, then the date step on the same card, Kemarin finishes", first, nil, func(h *contractHarness) {
			h.wantCard("Slip gaji ini dicatat sebagai apa?", "Alasan: Tanggal pembayaran tidak ada dan jenis gaji pertama perlu dipilih.")
			h.wantNoButton("Catat sebagai gaji")
			before, _, _ := h.current()
			h.tapLabel("Gaji utama")
			after, _, _ := h.current()
			if before != after {
				t.Fatalf("the date step moved the card from message %d to %d", before, after)
			}
			h.wantCard("✅ Jenis gaji disimpan.", "Tanggal pembayarannya kapan?")
			h.wantButtons("Hari ini", "Kemarin", "Tanggal lain", "Abaikan")
			h.wantSalary("", "", "", 0)
			h.tapLabel("Kemarin")
			label, monthDay := yesterdayLabel()
			h.wantCard(payslipDoneSalaryPrefix + label + ".")
			h.wantNoButtons()
			h.wantSalary("CONFIRMED", "8500000", monthDay, 1)
			h.wantResolution("SET_PAY_DATE")
			h.wantModelCalls(0)
		}},
		{"first payslip without a date: a typed 1/1 is the pay date", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			h.reply("1/1")
			h.wantCard(payslipDoneSalaryPrefix + "1 Jan.")
			h.wantSalary("CONFIRMED", "8500000", "01-01", 1)
			h.wantAnswered("TAP", "REPLY")
			h.wantModelCalls(0)
		}},
		{"first payslip without a date: the date as a thread reply", first, nil, func(h *contractHarness) {
			h.tapLabel("Pemasukan biasa")
			h.threadReply("1/1")
			h.wantCard("✅ Pemasukan tercatat · Rp8.500.000")
			h.wantSalary("CONFIRMED", "8500000", "01-01", 1)
			h.wantAnswered("TAP", "THREAD_REPLY")
		}},
		{"first payslip without a date: a future date is refused and the step is shown again", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			h.reply("25/12/2099")
			h.wantCard("Tanggal itu belum terjadi.", "Tanggal pembayarannya kapan?")
			h.wantButtons("Hari ini", "Kemarin", "Tanggal lain")
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
			h.plain("25/12/2099")
			h.wantCard("Tanggal itu belum terjadi.", "Tanggal pembayarannya kapan?")
			h.wantSalary("", "", "", 0)
		}},
		{"first payslip without a date: a year-less date in the next days is refused, never moved to last year", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			tomorrow := time.Now().In(jakartaLocation()).AddDate(0, 0, 1)
			h.reply(tomorrow.Format("2") + " " + strings.ToLower(contractMonths[tomorrow.Month()-1]))
			h.wantCard("Tanggal itu belum terjadi.", "Tanggal pembayarannya kapan?")
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
		}},
		{"first payslip without a date: a pay date older than the window is refused", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			h.reply("1/1/2020")
			h.wantCard("Tanggal itu terlalu lama untuk slip ini.", "Tanggal pembayarannya kapan?")
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
		}},
		{"first payslip without a date: Tanggal lain keeps the card and asks for the date", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			before, _, _ := h.current()
			h.tapLabel("Tanggal lain")
			after, _, _ := h.current()
			if before != after {
				t.Fatalf("Tanggal lain moved the card from message %d to %d", before, after)
			}
			h.wantCard("Ketik tanggalnya, misalnya 3 Okt atau 25/9.")
			h.wantButtons("Hari ini", "Kemarin")
			h.wantSalary("", "", "", 0)
		}},
		{"first payslip without a date: ya on the date step shows it again", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			h.reply("ya")
			h.wantCard("Maaf, aku belum menangkap itu.", "Tanggal pembayarannya kapan?")
			h.plain("oke")
			h.wantCard("Maaf, aku belum menangkap itu.", "Tanggal pembayarannya kapan?")
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
		}},
		{"first payslip without a date: an older choice button never records a second choice", first, nil, func(h *contractHarness) {
			data := h.buttonData("Gaji utama")
			h.tapLabel("Gaji utama")
			h.tap(data)
			h.wantCard("Pilihan itu tidak berlaku untuk slip ini.", "Tanggal pembayarannya kapan?")
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
		}},
		{"first payslip without a date: Abaikan on the date step records nothing", first, nil, func(h *contractHarness) {
			h.tapLabel("Gaji utama")
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: PT Contoh · Rp8.500.000. Tidak dicatat.")
			h.wantSalary("", "", "", 0)
			h.wantResolution("IGNORE")
		}},
		{"first payslip without a date: the other member's copy follows the card and closes", first, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			h.tapLabel("Gaji utama")
			if text := h.texts[messageB]; !strings.Contains(text, "Tanggal pembayarannya kapan?") {
				t.Fatalf("the other member's copy still shows the old step: %q", text)
			}
			h.tapLabel("Hari ini")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
		{"pay date only: the date step is first and Hari ini finishes it", first, []contractOption{withPrimarySalary()}, func(h *contractHarness) {
			h.wantCard("Tanggal pembayarannya kapan?", "Alasan: Tanggal pembayaran tidak tertulis di bukti.", "Gaji bersih: Rp8.500.000")
			h.wantButtons("Hari ini", "Kemarin", "Tanggal lain", "Abaikan")
			h.tapLabel("Hari ini")
			h.wantCard(payslipDoneSalaryPrefix)
			h.wantSalary("CONFIRMED", "8500000", time.Now().In(jakartaLocation()).Format("01-02"), 1)
			h.wantResolution("SET_PAY_DATE")
		}},
		{"pay date only: a plain date binds to the single waiting date card", first, []contractOption{withPrimarySalary()}, func(h *contractHarness) {
			h.plain("1/1")
			h.wantCard(payslipDoneSalaryPrefix + "1 Jan.")
			h.wantSalary("CONFIRMED", "8500000", "01-01", 1)
			h.wantAnswered("PLAIN")
			h.wantAssistantCalls(0)
		}},
		{"pay date only: a plain date with two date cards waiting asks which and records nothing", first, []contractOption{withPrimarySalary()}, func(h *contractHarness) {
			secondItem, _ := h.seedPayslip("second")
			first := h.reviewID
			h.project(secondItem)
			h.reviewID = first
			h.chat("1/1")
			h.wantAssistantCalls(0)
			h.wantSalary("", "", "", 0)
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text' LIKE 'Ada beberapa kartu%' AND (payload_json->>'chat_id')::bigint=$1`, 1, h.f.chatID)
		}},
		{"pay date only: a question goes to the assistant", first, []contractOption{withPrimarySalary()}, func(h *contractHarness) {
			h.chat("tanggal gajian itu kapan ya?")
			h.wantAssistantCalls(1)
			h.wantSalary("", "", "", 0)
			h.wantItemActive()
		}},
	})
}

func TestInteractionEngineCycleContract(t *testing.T) {
	const cycle = "CYCLE_RESIDUAL_ALLOCATION"
	both := withSavingsAccounts("Tabungan Contoh")
	runContractRows(t, []contractRow{
		{"the leftover card asks where to put it and offers only savings and investment accounts", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh"), withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.wantCard("Sisa siklus 25 Agu–24 Sep: Rp1.200.000. Mau dialokasikan ke mana?", "Alasan: Alokasi sisa saldo adalah keputusan keluarga, jadi tidak ditebak.")
			h.wantButtonLabels("Reksa Contoh", "Tabungan Contoh", "Bagi ke beberapa akun", "Biarkan saja", "Ada transaksi belum tercatat")
			h.wantModelCalls(0)
		}},
		{"a tapped account takes the whole leftover", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Tabungan Contoh")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke Tabungan Contoh.")
			h.wantNoButtons()
			h.wantAllocations("Tabungan Contoh 1200000")
			h.wantResolution("ALLOCATE_RETAINED_BALANCE")
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"an account name typed as a reply takes the whole leftover", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.reply("  TABUNGAN contoh ")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke Tabungan Contoh.")
			h.wantAllocations("Tabungan Contoh 1200000")
			h.wantAnswered("LABEL_TEXT")
			h.wantModelCalls(0)
		}},
		{"an account name typed plainly takes the whole leftover", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.plain("reksa contoh")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke Reksa Contoh.")
			h.wantAllocations("Reksa Contoh 1200000")
			h.wantAssistantCalls(0)
		}},
		{"an account name thread-replied takes the whole leftover", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.threadReply("Reksa Contoh")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke Reksa Contoh.")
			h.wantAllocations("Reksa Contoh 1200000")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"ya typed or replied to the leftover card shows it again and records nothing", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.reply("ya")
			h.wantCard("Maaf, aku belum menangkap itu.", "Mau dialokasikan ke mana?")
			h.wantButtons("Tabungan Contoh", "Bagi ke beberapa akun", "Biarkan saja", "Ada transaksi belum tercatat")
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu.", "Mau dialokasikan ke mana?")
			h.wantButtons("Tabungan Contoh", "Biarkan saja")
			h.plain("oke")
			h.wantCard("Mau dialokasikan ke mana?")
			h.threadReply("alokasikan ke tabungan contoh ya")
			h.wantCard("Mau dialokasikan ke mana?")
			h.wantAllocations()
			h.wantItemActive()
			h.wantAssistantCalls(0)
			h.wantModelCalls(0)
		}},
		{"Biarkan saja typed as text leaves the leftover unallocated", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.reply("biarkan saja")
			h.wantCard("✅ Sisa Rp1.200.000 dibiarkan tidak dialokasikan.")
			h.wantNoButtons()
			h.wantAllocations()
			h.wantResolution("LEAVE_UNALLOCATED")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"a split across two accounts with Semua sisa applies both allocations at once", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			before, _, _ := h.current()
			h.tapLabel("Bagi ke beberapa akun")
			h.wantCard("Bagi sisa Rp1.200.000. Pilih akun pertama.")
			h.wantButtonLabels("Reksa Contoh", "Tabungan Contoh", "Kembali", "Batal")
			h.tapLabel("Tabungan Contoh")
			h.wantCard("Berapa ke Tabungan Contoh? Sisa Rp1.200.000", "500rb")
			h.wantButtonLabels("Semua sisa", "Kembali", "Batal")
			h.reply("500rb")
			h.wantCard("✅ Rp500.000 dialokasikan.", "Tabungan Contoh: Rp500.000", "Sisa Rp700.000 belum dibagi. Pilih akun berikutnya.")
			h.wantButtonLabels("Reksa Contoh", "Kembali", "Batal")
			h.wantAllocations()
			h.tapLabel("Reksa Contoh")
			h.wantCard("Berapa ke Reksa Contoh? Sisa Rp700.000")
			h.tapLabel("Semua sisa")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke 2 akun: Tabungan Contoh Rp500.000, Reksa Contoh Rp700.000.")
			h.wantNoButtons()
			h.wantAllocations("Reksa Contoh 700000", "Tabungan Contoh 500000")
			h.wantResolution("ALLOCATE_RETAINED_BALANCE")
			if after, _, _ := h.current(); after == before {
				t.Fatal("a typed amount edited the card instead of sending a fresh one")
			}
			h.wantModelCalls(0)
		}},
		{"a split amount above the remainder or unreadable is refused and the step shown again", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.reply("2jt")
			h.wantCard("Jumlahnya harus lebih dari nol dan tidak lebih dari sisa Rp1.200.000.", "Berapa ke Tabungan Contoh?")
			h.reply("500rb")
			h.tapLabel("Reksa Contoh")
			h.reply("1,5jt")
			h.wantCard("tidak lebih dari sisa Rp700.000.", "Berapa ke Reksa Contoh?")
			h.reply("750000")
			h.wantCard("tidak lebih dari sisa Rp700.000.")
			h.reply("0")
			h.wantCard("Jumlahnya belum terbaca.", "Berapa ke Reksa Contoh?")
			h.reply("ya")
			h.wantCard("Maaf, aku belum menangkap itu.", "Berapa ke Reksa Contoh?")
			h.wantAllocations()
			h.wantItemActive()
			h.reply("700000")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke 2 akun")
			h.wantAllocations("Reksa Contoh 700000", "Tabungan Contoh 500000")
		}},
		{"the last unused account must take the whole remainder", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.reply("500rb")
			h.tapLabel("Reksa Contoh")
			h.reply("100rb")
			h.wantCard("Ini akun terakhir, jadi harus menampung seluruh sisa Rp700.000.", "Berapa ke Reksa Contoh?")
			h.wantAllocations()
			h.tapLabel("Semua sisa")
			h.wantAllocations("Reksa Contoh 700000", "Tabungan Contoh 500000")
		}},
		{"a leftover lowered mid-split restarts the card instead of showing a negative remainder", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.reply("500rb")
			h.exec(`UPDATE cycle_residual_case SET basis_residual_idr=400000 WHERE id=$1`, h.caseID)
			h.tapLabel("Reksa Contoh")
			h.wantCard("Sisa siklus 25 Agu–24 Sep: Rp400.000. Mau dialokasikan ke mana?")
			if _, text, _ := h.current(); strings.Contains(text, "-Rp") {
				t.Fatalf("a negative remainder was shown: %q", text)
			}
			h.wantButtons("Tabungan Contoh", "Bagi ke beberapa akun", "Biarkan saja")
			h.wantAllocations()
			h.wantItemActive()
		}},
		{"a split can take more than a hundred million from a large leftover", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh"), withLargeLeftover()}, func(h *contractHarness) {
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.reply("120jt")
			h.wantCard("✅ Rp120.000.000 dialokasikan.", "Sisa Rp30.000.000 belum dibagi.")
			h.tapLabel("Reksa Contoh")
			h.reply("30.000.000")
			h.wantCard("✅ Sisa Rp150.000.000 dialokasikan ke 2 akun")
			h.wantAllocations("Reksa Contoh 30000000", "Tabungan Contoh 120000000")
		}},
		{"Batal mid-split clears it and returns to the first step", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			before, _, _ := h.current()
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.reply("500rb")
			h.tapLabel("Batal")
			h.wantCard("Mau dialokasikan ke mana?")
			h.wantButtons("Tabungan Contoh", "Reksa Contoh", "Bagi ke beberapa akun")
			h.wantAllocations()
			h.wantItemActive()
			if after, _, _ := h.current(); after == before {
				t.Fatal("the reply did not produce a fresh card")
			}
			h.tapLabel("Reksa Contoh")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke Reksa Contoh.")
			h.wantAllocations("Reksa Contoh 1200000")
		}},
		{"Kembali goes back one step at a time", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.tapLabel("Kembali")
			h.wantCard("Bagi sisa Rp1.200.000. Pilih akun pertama.")
			h.wantButtons("Tabungan Contoh", "Reksa Contoh")
			h.tapLabel("Tabungan Contoh")
			h.reply("500rb")
			h.tapLabel("Kembali")
			h.wantCard("Berapa ke Tabungan Contoh? Sisa Rp1.200.000")
			h.tapLabel("Kembali")
			h.tapLabel("Kembali")
			h.wantCard("Mau dialokasikan ke mana?")
			h.wantButtons("Bagi ke beberapa akun", "Biarkan saja")
			h.wantAllocations()
		}},
		{"Ada transaksi belum tercatat resolves the card and the next plain message is ordinary chat", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Ada transaksi belum tercatat")
			h.wantCard("Ketik transaksi yang belum tercatat, misalnya 'makan 25rb kemarin'. Sisa siklus akan dihitung ulang.")
			h.wantNoButtons()
			h.wantAllocations()
			h.wantResolution("TRANSACTION_MISSING")
			h.chat("makan 25rb kemarin")
			h.wantAssistantCalls(1)
		}},
		{"a question about the leftover goes to the assistant", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.question(messageID, "sisa ini maksudnya apa?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantAllocations()
			h.wantItemActive()
		}},
		{"untargeted chat is not swallowed by the leftover card", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			h.wantAllocations()
			h.wantItemActive()
		}},
		{"a button from an older copy of the account list is refused", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			old := h.buttonData("Tabungan Contoh")
			h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Aaa Lain','ASSET','DEPOSIT','SAVINGS')`, h.f.householdID)
			h.tap("review:cycpage:0")
			h.wantButtons("Aaa Lain", "Tabungan Contoh")
			h.tap(old)
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Mau dialokasikan ke mana?")
			h.wantAllocations()
			h.wantItemActive()
		}},
		{"an account deactivated after the buttons were shown is refused", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			data := h.buttonData("Tabungan Contoh")
			h.exec(`UPDATE wealth_account SET active=false WHERE household_id=$1 AND name='Tabungan Contoh'`, h.f.householdID)
			h.tap(data)
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Mau dialokasikan ke mana?")
			h.wantNoButton("Tabungan Contoh")
			h.wantAllocations()
		}},
		{"a recomputed leftover shows the new amount and never applies the stale one", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',200000,'2026-09-02T12:00:00+07:00',now())`, h.f.householdID)
			h.tapLabel("Tabungan Contoh")
			h.wantCard("Sisa siklus berubah. Pilih lagi.", "Sisa siklus 25 Agu–24 Sep: Rp1.000.000.")
			h.wantButtons("Tabungan Contoh", "Reksa Contoh", "Biarkan saja")
			h.wantAllocations()
			h.wantItemActive()
			h.tapLabel("Tabungan Contoh")
			h.wantCard("✅ Sisa Rp1.000.000 dialokasikan ke Tabungan Contoh.")
			h.wantAllocations("Tabungan Contoh 1000000")
		}},
		{"a leftover that vanished closes the card without allocating", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',1200000,'2026-09-02T12:00:00+07:00',now())`, h.f.householdID)
			h.tapLabel("Biarkan saja")
			h.wantCard("Sisa siklus sudah tidak ada lagi. Tinjauan ditutup.")
			h.wantNoButtons()
			h.wantAllocations()
			h.wantItem("RESOLVED")
		}},
		{"a split that went stale restarts instead of applying a mismatched total", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh")}, func(h *contractHarness) {
			h.tapLabel("Bagi ke beberapa akun")
			h.tapLabel("Tabungan Contoh")
			h.reply("500rb")
			h.tapLabel("Reksa Contoh")
			h.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',200000,'2026-09-02T12:00:00+07:00',now())`, h.f.householdID)
			h.tapLabel("Semua sisa")
			h.wantCard("Sisa siklus berubah. Pilih lagi.", "Rp1.000.000.")
			h.wantAllocations()
			h.wantItemActive()
		}},
		{"with no savings account only the closing choices remain", cycle, nil, func(h *contractHarness) {
			h.wantCard("Belum ada akun tabungan atau investasi yang aktif")
			h.wantButtonLabels("Biarkan saja", "Ada transaksi belum tercatat")
		}},
		{"accounts are paged in place and a page two pick lands", cycle, []contractOption{withSavingsAccounts("Akun 01", "Akun 02", "Akun 03", "Akun 04", "Akun 05", "Akun 06", "Akun 07", "Akun 08", "Akun 09", "Akun 10")}, func(h *contractHarness) {
			before, _, _ := h.current()
			h.wantNoButton("Akun 09")
			h.tapLabel("Berikutnya")
			after, _, _ := h.current()
			if before != after {
				t.Fatalf("paging moved the card from message %d to %d", before, after)
			}
			h.wantCard("(halaman 2)")
			h.wantButtons("Akun 09", "Akun 10", "Sebelumnya")
			h.tapLabel("Akun 10")
			h.wantCard("✅ Sisa Rp1.200.000 dialokasikan ke Akun 10.")
			h.wantAllocations("Akun 10 1200000")
		}},
		{"answering on one phone updates then closes the other member's copy", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh"), withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			if messageB == 0 {
				t.Fatal("the second member never received the card")
			}
			h.tapLabel("Bagi ke beberapa akun")
			if text := h.texts[messageB]; !strings.Contains(text, "Pilih akun pertama.") {
				t.Fatalf("the other member's copy still shows the old step: %q", text)
			}
			h.tapLabel("Batal")
			h.tapLabel("Tabungan Contoh")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
			h.wantAllocations("Tabungan Contoh 1200000")
		}},
		{"a tap from a finished card's older copy allocates nothing twice", cycle, []contractOption{both, withInvestmentAccounts("Reksa Contoh"), withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Tabungan Contoh")
			h.tapLabel("Tabungan Contoh")
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantAllocations("Tabungan Contoh 1200000")
			h.wantResolution("ALLOCATE_RETAINED_BALANCE")
		}},
	})
}
