package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// Integration tests for what a Telegram review card says and can do: the card
// is the whole interaction, every button it shows can execute, and a re-render
// keeps the remaining actions.

type cardFixture struct {
	t         *testing.T
	ctx       context.Context
	pool      *pgxpool.Pool
	household string
	user      string
	chat      int64
}

func newCardFixture(t *testing.T) *cardFixture {
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
	f := &cardFixture{t: t, ctx: ctx, pool: pool, chat: time.Now().UnixNano()}
	f.row(`INSERT INTO household(name) VALUES($1) RETURNING id`, []any{&f.household}, fmt.Sprintf("Cards %d", f.chat))
	f.row(`INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, []any{&f.user}, fmt.Sprintf("cards-%d@example.test", f.chat))
	f.exec(`INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, f.household, f.user)
	f.exec(`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, f.chat, f.household, f.user)
	return f
}

func (f *cardFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("%v\n%s", err, sql)
	}
}

func (f *cardFixture) row(sql string, dest []any, args ...any) {
	f.t.Helper()
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(dest...); err != nil {
		f.t.Fatalf("%v\n%s", err, sql)
	}
}

func (f *cardFixture) id(sql string, args ...any) string {
	f.t.Helper()
	var id string
	f.row(sql, []any{&id}, args...)
	return id
}

// bindCard creates the open Telegram projection of an item as delivered card
// messageID, with the conversation in state.
func (f *cardFixture) bindCard(itemID, transactionID, reviewType, state string, messageID int64, text string) string {
	f.t.Helper()
	request := f.id(`INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,NULLIF($3,'')::uuid,$4,'OPEN',$5) RETURNING id`, f.household, itemID, transactionID, reviewType, f.chat)
	f.exec(`INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id,delivered_text) VALUES($1,$2,$3,$4)`, request, f.chat, messageID, text)
	f.exec(`INSERT INTO review_conversation(review_request_id,state) VALUES($1,$2)`, request, state)
	return request
}

// tap processes one button press on card messageID, carrying the card text the
// way Telegram's callback does.
func (f *cardFixture) tap(messageID int64, data, text string) {
	f.t.Helper()
	source := seedTelegramRaw(f.t, f.pool, f.household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": map[string]any{
		"id": "cb", "data": data, "from": map[string]any{"id": f.chat},
		"message": map[string]any{"message_id": messageID, "text": text, "chat": map[string]any{"id": f.chat}},
	}})
	if err := NewProcessor(f.pool, nil).Process(f.ctx, source); err != nil {
		f.t.Fatalf("tap %s: %v", data, err)
	}
}

func (f *cardFixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	f.row(sql, []any{&n}, args...)
	return n
}

func (f *cardFixture) sendsToChat() int {
	return f.count(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, f.chat)
}

func (f *cardFixture) lastEdit(messageID int64) (string, string) {
	f.t.Helper()
	var text, markup string
	f.row(`SELECT payload_json->>'text',COALESCE(payload_json->'reply_markup','null'::jsonb)::text FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, []any{&text, &markup}, f.chat, messageID)
	return text, markup
}

// R1: one expense review is one card. The category tap turns the card into
// its result with the remember choice inline; the choice settles it again.
// No extra message is sent and no button is left behind.
func TestCategoryTapSettlesOnTheCard(t *testing.T) {
	f := newCardFixture(t)
	category := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Galon Isi Ulang','galon') RETURNING id`, f.household)
	merchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, f.household)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',23000,now(),$2) RETURNING id`, f.household, merchant)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	request := f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", 17, "🟡 Kategori belum pasti")
	sendsBefore := f.sendsToChat()

	f.tap(17, "review:cat:"+category, "🟡 Kategori belum pasti")
	text, markup := f.lastEdit(17)
	if !strings.HasPrefix(text, "✅ Dicatat: Galon Isi Ulang") || !strings.Contains(text, "Nominal: Rp23.000") || !strings.Contains(text, "Tins Mart") || !strings.Contains(markup, "review:remember") || !strings.Contains(markup, "review:once") {
		t.Fatalf("card after tap: %q %s", text, markup)
	}
	if pending := f.count(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND status='PENDING' AND payload_json->>'review_request_id'=$1`, request); pending != 0 {
		t.Fatalf("the settled card still has %d pending retirements that would strip the remember choice", pending)
	}

	f.tap(17, "review:remember", text)
	text, markup = f.lastEdit(17)
	if !strings.HasPrefix(text, "✅ Dicatat: Galon Isi Ulang") || !strings.Contains(text, "Diingat untuk Tins Mart") || strings.Contains(markup, "review:") {
		t.Fatalf("card after remember: %q %s", text, markup)
	}
	if sends := f.sendsToChat(); sends != sendsBefore {
		t.Fatalf("the review sent %d extra messages; it must settle on the card", sends-sendsBefore)
	}
	if auto := f.count(`SELECT count(*) FROM merchant_alias WHERE household_id=$1 AND normalized_merchant_id=$2 AND default_category_id=$3 AND auto_apply`, f.household, merchant, category); auto != 1 {
		t.Fatalf("remember did not store the merchant category: %d", auto)
	}
	var state string
	f.row(`SELECT state FROM review_conversation WHERE review_request_id=$1`, []any{&state}, request)
	if state != "RESOLVED" {
		t.Fatalf("conversation=%s", state)
	}
}

// R2: a merchant that sells several kinds of things gets its own past
// categories as the first row, most used first, with the category that every
// earlier purchase at exactly this amount used marked as the suggestion.
func TestCategoryChooserLeadsWithMerchantHistory(t *testing.T) {
	f := newCardFixture(t)
	galon := f.id(`INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,'Galon Isi Ulang','galon',9) RETURNING id`, f.household)
	kopi := f.id(`INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,'Kopi & Camilan','kopi',8) RETURNING id`, f.household)
	gas := f.id(`INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,'Gas','gas',7) RETURNING id`, f.household)
	other := f.id(`INSERT INTO category(household_id,name,slug,sort_order) VALUES($1,'Makan','makan',1) RETURNING id`, f.household)
	merchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, f.household)
	confirmed := func(category string, amount int, daysAgo int) {
		f.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id,category_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',$2,now()-make_interval(days=>$3),$4,$5,now())`, f.household, amount, daysAgo, merchant, category)
	}
	confirmed(kopi, 15000, 1)
	confirmed(kopi, 18000, 2)
	confirmed(galon, 34500, 10)
	confirmed(galon, 34500, 11)
	confirmed(galon, 23000, 12)
	confirmed(gas, 23000, 20)
	// Another household's history for a same-named merchant must not leak in.
	foreign := f.id(`INSERT INTO household(name) VALUES('foreign') RETURNING id`)
	foreignCategory := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Asing','asing') RETURNING id`, foreign)
	foreignMerchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, foreign)
	f.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id,category_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',34500,now(),$2,$3,now())`, foreign, foreignMerchant, foreignCategory)

	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34500,now(),$2) RETURNING id`, f.household, merchant)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	request := f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", 21, "card")
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	markup := categoryChooserMarkup(f.ctx, tx, request, 0)
	if markup == nil || len(markup.InlineKeyboard) < 2 {
		t.Fatalf("markup=%#v", markup)
	}
	first := markup.InlineKeyboard[0]
	want := []string{"⭐ Galon Isi Ulang", "Kopi & Camilan", "Gas"}
	wantIDs := []string{galon, kopi, gas}
	if len(first) != 3 {
		t.Fatalf("history row=%#v", first)
	}
	for index := range want {
		if first[index].Text != want[index] || first[index].CallbackData != "review:cat:"+wantIDs[index] {
			t.Fatalf("history row[%d]=%#v, want %s", index, first[index], want[index])
		}
	}
	encoded, _ := json.Marshal(markup.InlineKeyboard[1:])
	if strings.Contains(string(encoded), galon) || strings.Contains(string(encoded), foreignCategory) || !strings.Contains(string(encoded), other) {
		t.Fatalf("generic rows repeat history or leak another household: %s", encoded)
	}

	// At 23000 the history is split between Galon and Gas, so nothing is starred.
	f.exec(`UPDATE transaction SET amount=23000 WHERE id=$1`, transaction)
	markup = categoryChooserMarkup(f.ctx, tx, request, 0)
	encoded, _ = json.Marshal(markup.InlineKeyboard[0])
	if strings.Contains(string(encoded), "⭐") {
		t.Fatalf("an amount with two past categories must not be suggested: %s", encoded)
	}
}

// R4: Abaikan executes for every kind whose contract allows IGNORE, and a
// kind that cannot be ignored never shows the button.
func TestIgnoreExecutesForNonTransactionKinds(t *testing.T) {
	f := newCardFixture(t)
	t.Run("missing amount", func(t *testing.T) {
		source := f.id(`INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.household, fmt.Sprintf("amount-%d", f.chat), []byte("amount"))
		proposal := f.id(`INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,transaction_at,confidence,proposal_status,merchant_raw,metadata_json) VALUES($1,$2,'EXPENSE',NULL,now(),.9,'NEEDS_REVIEW','Warung',jsonb_build_object('document_id',gen_random_uuid())) RETURNING id`, f.household, source)
		item := f.id(`INSERT INTO review_item(household_id,proposal_id,review_type,status,decision) VALUES($1,$2,'MISSING_AMOUNT','OPEN','{"version":1,"reasonCode":"MISSING_AMOUNT","missingFacts":["amount"],"allowedActions":["SET_AMOUNT","IGNORE"]}'::jsonb) RETURNING id`, f.household, proposal)
		f.bindCard(item, "", "MISSING_AMOUNT", "AWAITING_DETAIL", 31, "card")
		f.tap(31, "review:ignore", "card")
		var status, proposalStatus string
		f.row(`SELECT ri.status,p.proposal_status FROM review_item ri JOIN transaction_proposal p ON p.id=ri.proposal_id WHERE ri.id=$1`, []any{&status, &proposalStatus}, item)
		if status != "RESOLVED" || proposalStatus != "REJECTED" {
			t.Fatalf("missing amount ignore: item=%s proposal=%s", status, proposalStatus)
		}
	})
	t.Run("wealth observation", func(t *testing.T) {
		observation := f.id(`INSERT INTO wealth_observation(household_id,institution,account_hint,observed_value_idr) VALUES($1,'Bibit','RDPU',1500000) RETURNING id`, f.household)
		item := f.id(`INSERT INTO review_item(household_id,wealth_observation_id,review_type,status,decision) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN','{"version":1,"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","missingFacts":["wealth_snapshot_confirmation"],"allowedActions":["SET_WEALTH_ACCOUNT","IGNORE"]}'::jsonb) RETURNING id`, f.household, observation)
		f.bindCard(item, "", "WEALTH_OBSERVATION_CONFIRMATION", "AWAITING_DETAIL", 32, "card")
		f.tap(32, "review:ignore", "card")
		var status, observationStatus string
		f.row(`SELECT ri.status,wo.status FROM review_item ri JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id WHERE ri.id=$1`, []any{&status, &observationStatus}, item)
		if status != "RESOLVED" || observationStatus != "DISMISSED" {
			t.Fatalf("wealth ignore: item=%s observation=%s", status, observationStatus)
		}
	})
	t.Run("cycle residual has no Abaikan and leaves unallocated", func(t *testing.T) {
		salarySource := f.id(`INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'ACME','acme',true) RETURNING id`, f.household, f.user)
		source := f.id(`INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, f.household, fmt.Sprintf("cycle-%d", f.chat))
		salary := func(period string) string {
			// Salary transactions sit outside the window so the basis is exactly
			// the one income below.
			paid := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1,'2025-01-01',now()) RETURNING id`, f.household)
			return f.id(`INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1,$4,'CONFIRMED',$5) RETURNING id`, salarySource, f.household, period, paid, source)
		}
		start, end := salary("2026-08-25"), salary("2026-09-25")
		f.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1250000,'2026-09-01T12:00:00+07:00',now())`, f.household)
		// The stored basis matches the recomputed one, so the tap is not stale.
		caseID := f.id(`INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-08-25','2026-09-25',1250000,0,0,1250000) RETURNING id`, f.household, start, end)
		item := f.id(`INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','PENDING_SEND','{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","decisionClass":"HUMAN_POLICY_CHOICE","missingFacts":["residual_allocation"],"allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"],"interactionMode":"POLICY_CHOICE"}'::jsonb) RETURNING id`, f.household, caseID)
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := ProjectReviewMessage(f.ctx, tx, f.household, item, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		var text, markup, request string
		f.row(`SELECT payload_json->>'text',payload_json->'reply_markup'::text,payload_json->>'review_request_id' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id=$1)`, []any{&text, &markup, &request}, item)
		if !strings.Contains(text, "Sisa: Rp1.250.000") || !strings.Contains(text, "Periode: 25 Agu 2026 – 24 Sep 2026") || !strings.Contains(text, "Alasan: ") || strings.Contains(markup, "review:ignore") || !strings.Contains(markup, "review:leave") {
			t.Fatalf("cycle card: %q %s", text, markup)
		}
		f.exec(`UPDATE review_request_recipient SET telegram_message_id=33 WHERE review_request_id=$1`, request)
		f.tap(33, "review:leave", text)
		var status, action string
		f.row(`SELECT status,COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, []any{&status, &action}, item)
		if status != "RESOLVED" || action != "LEAVE_UNALLOCATED" {
			t.Fatalf("leave unallocated: status=%s action=%s", status, action)
		}
	})
}

// Item 8: a transfer review bound to a source event and a reconciliation case
// renders its chooser from the case, and the chooser executes.
func TestSourceBoundTransferCardHasExecutableButtons(t *testing.T) {
	f := newCardFixture(t)
	account := f.id(`INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'BCA','BANK','FULL_LEDGER') RETURNING id`, f.household)
	candidate := f.id(`INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at) VALUES($1,$2,'UNCLASSIFIED','NEEDS_REVIEW',500000,now()-interval '1 day') RETURNING id`, f.household, account)
	source := f.id(`INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.household, fmt.Sprintf("xfer-%d", f.chat), []byte("xfer"))
	f.exec(`INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,candidate_transaction_ids) VALUES($1,$2,$3,500000,now(),'ke tabungan','INTERNAL_TRANSFER',ARRAY[$4::uuid])`, f.household, source, account, candidate)
	item := f.id(`INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","decisionClass":"EVIDENCE_GAP","missingFacts":["transfer_relationship"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}'::jsonb) RETURNING id`, f.household, source)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProjectReviewItem(f.ctx, tx, f.household, item, 0, "", f.chat); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var text, markup, request string
	f.row(`SELECT payload_json->>'text',payload_json->'reply_markup'::text,payload_json->>'review_request_id' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id=$1)`, []any{&text, &markup, &request}, item)
	for _, want := range []string{"review:xfer:merge:0", "review:xfer:new", "review:ignore"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("transfer card lacks %s: %q %s", want, text, markup)
		}
	}
	if !strings.Contains(text, "Nominal: Rp500.000") || strings.Contains(markup, candidate) {
		t.Fatalf("transfer card text/markup: %q %s", text, markup)
	}
	f.exec(`UPDATE review_request_recipient SET telegram_message_id=41 WHERE review_request_id=$1`, request)
	f.tap(41, "review:xfer:merge:0", text)
	var status, candidateStatus string
	f.row(`SELECT ri.status,t.status FROM review_item ri, transaction t WHERE ri.id=$1 AND t.id=$2`, []any{&status, &candidateStatus}, item, candidate)
	if status != "RESOLVED" || candidateStatus != "CONFIRMED" {
		t.Fatalf("merge: item=%s candidate=%s", status, candidateStatus)
	}
}

// R10: the income card's Benar confirms income; Ubah offers the
// income/transfer choices on the same card, and those execute.
func TestIncomeCardConfirmAndChangeExecute(t *testing.T) {
	f := newCardFixture(t)
	income := func(messageID int64) (string, string) {
		transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',750000,now()) RETURNING id`, f.household)
		item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
		f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", messageID, "card")
		return transaction, item
	}
	confirmed, _ := income(51)
	f.tap(51, "review:confirm", "card")
	var status, kind string
	f.row(`SELECT status,type FROM transaction WHERE id=$1`, []any{&status, &kind}, confirmed)
	if status != "CONFIRMED" || kind != "INCOME" {
		t.Fatalf("Benar: status=%s type=%s", status, kind)
	}
	changed, _ := income(52)
	f.tap(52, "review:change", "card")
	text, markup := f.lastEdit(52)
	if !strings.Contains(markup, "review:own") || !strings.Contains(markup, "review:income") || !strings.Contains(text, "Rp750.000") {
		t.Fatalf("Ubah card: %q %s", text, markup)
	}
	f.tap(52, "review:own", text)
	f.row(`SELECT status,type FROM transaction WHERE id=$1`, []any{&status, &kind}, changed)
	if status != "CONFIRMED" || kind != "TRANSFER" {
		t.Fatalf("Ubah > Rekening sendiri: status=%s type=%s", status, kind)
	}
}

// Item 6: a card re-rendered after a partial answer keeps its remaining
// actions instead of being sent with an empty keyboard.
func TestReRenderedCardKeepsItsRemainingActions(t *testing.T) {
	f := newCardFixture(t)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',18502,now()) RETURNING id`, f.household)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'MISSING_TRANSACTION_DATE','OPEN','{"version":1,"reasonCode":"MISSING_TRANSACTION_DATE","missingFacts":["transaction_at"],"allowedActions":["SET_PAY_DATE","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	request := f.bindCard(item, transaction, "MISSING_TRANSACTION_DATE", "AWAITING_DATE", 61, "card")
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	update := callbackUpdate(f.chat, 61, "review:cat:x")
	if err := refreshReviewFactsProjection(f.ctx, tx, f.household, request, update); err != nil {
		t.Fatal(err)
	}
	var text, markup string
	if err := tx.QueryRow(f.ctx, `SELECT payload_json->>'text',payload_json->'reply_markup'::text FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1`, request).Scan(&text, &markup); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "review:ignore") || !strings.Contains(text, "Nominal: Rp18.502") || !strings.Contains(text, "Alasan: ") {
		t.Fatalf("re-rendered card: %q %s", text, markup)
	}
}

// Item 10: a stale tap on a card that no longer belongs to a live review loses
// its buttons; a stale choice on a still-open card keeps them.
func TestStaleTapRetiresOnlyDeadCards(t *testing.T) {
	f := newCardFixture(t)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',1000,now(),now()) RETURNING id`, f.household)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	request := f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "RESOLVED", 71, "old card")
	f.exec(`UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, request)
	f.tap(71, "review:edit", "old card")
	text, markup := f.lastEdit(71)
	if !strings.HasPrefix(text, "old card") || strings.Contains(markup, "review:") {
		t.Fatalf("stale card kept its buttons: %q %s", text, markup)
	}
}

// Item 11: a reopened review gets a fresh decision and is projected through
// the shared path by its job.
func TestReopenedReviewIsProjectedWithAFreshDecision(t *testing.T) {
	f := newCardFixture(t)
	category := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, f.household)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id,category_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',42000,now(),$2,$3) RETURNING id`, f.household, f.user, category)
	// The earlier review asked for the date and category, which were answered
	// before the merge; reversing it must not ask them again.
	f.exec(`INSERT INTO review_item(household_id,transaction_id,review_type,status,resolved_at,decision) VALUES($1,$2,'TRANSACTION_FACTS_MISSING','RESOLVED',now(),'{"version":1,"reasonCode":"TRANSACTION_FACTS_MISSING","knownFacts":{"amount_idr":"1"},"missingFacts":["category","transaction_at"],"allowedActions":["CONFIRM_REVIEW","SET_PAY_DATE","IGNORE"]}'::jsonb)`, f.household, transaction)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewdomain.ReopenTransactionReview(f.ctx, tx, f.household, transaction); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var item, amount, raw string
	f.row(`SELECT id::text,decision->'knownFacts'->>'amount_idr' FROM review_item WHERE transaction_id=$1 AND status='OPEN'`, []any{&item, &amount}, transaction)
	if amount != "42000" {
		t.Fatalf("reopened decision reused the old snapshot: amount_idr=%s", amount)
	}
	var missing, reason string
	f.row(`SELECT decision->'missingFacts'::text,review_type FROM review_item WHERE id=$1`, []any{&missing, &reason}, item)
	if missing != `["duplicate_relationship"]` || reason != "POSSIBLE_DUPLICATE" {
		t.Fatalf("reopened review asks answered facts again: type=%s missing=%s", reason, missing)
	}
	f.row(`SELECT payload_json::text FROM job WHERE type='PROJECT_TELEGRAM_REVIEW' AND payload_json->>'review_item_id'=$1`, []any{&raw}, item)
	payload, err := DecodeProjectReviewPayload(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(f.pool, nil)
	for range 2 {
		if err := processor.ProjectReviewJob(f.ctx, payload); err != nil {
			t.Fatal(err)
		}
	}
	if sends := f.count(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id=$1)`, item); sends != 1 {
		t.Fatalf("reopened review projected %d times, want once", sends)
	}
}

// A merchant already confirmed with another category is not offered a single
// remembered default: it would miscategorize its other purchases.
func TestMultiCategoryMerchantIsNotOfferedARememberedDefault(t *testing.T) {
	f := newCardFixture(t)
	galon := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Galon Isi Ulang','galon') RETURNING id`, f.household)
	kopi := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Kopi & Camilan','kopi') RETURNING id`, f.household)
	merchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, f.household)
	f.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id,category_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',15000,now(),$2,$3,now())`, f.household, merchant, kopi)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',23000,now(),$2) RETURNING id`, f.household, merchant)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	request := f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", 81, "card")
	f.tap(81, "review:cat:"+galon, "card")
	text, markup := f.lastEdit(81)
	if !strings.HasPrefix(text, "✅ Dicatat: Galon Isi Ulang") || strings.Contains(markup, "review:remember") || strings.Contains(text, "?") {
		t.Fatalf("multi-category merchant card: %q %s", text, markup)
	}
	var state string
	f.row(`SELECT state FROM review_conversation WHERE review_request_id=$1`, []any{&state}, request)
	if state != "RESOLVED" {
		t.Fatalf("conversation=%s", state)
	}
}

// Outcome edits of one card apply only at their newest revision: a delayed or
// retried question edit that runs after the final answer is dropped, so the
// answered remember buttons never come back.
func TestOutOfOrderOutcomeEditsCannotRestoreButtons(t *testing.T) {
	f := newCardFixture(t)
	category := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Galon Isi Ulang','galon') RETURNING id`, f.household)
	merchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, f.household)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',23000,now(),$2) RETURNING id`, f.household, merchant)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", 91, "card")
	f.tap(91, "review:cat:"+category, "card")
	f.tap(91, "review:once", "question")
	rows, err := f.pool.Query(f.ctx, `SELECT payload_json::text FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=91 ORDER BY (payload_json->>'outcome_revision')::int`, f.chat)
	if err != nil {
		t.Fatal(err)
	}
	var payloads []EditPayload
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		payload, err := DecodeEditPayload(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, payload)
	}
	rows.Close()
	if len(payloads) != 2 || payloads[0].OutcomeRevision != 1 || payloads[1].OutcomeRevision != 2 {
		t.Fatalf("outcome edits=%+v", payloads)
	}
	bot, fake := newFakeTelegram(t, func(string) (int, string) { return 200, `{"ok":true,"result":true}` })
	processor := NewProcessor(f.pool, nil)
	// The final answer is delivered first, then the delayed question edit and a
	// retry of it run.
	for _, payload := range []EditPayload{payloads[1], payloads[0], payloads[0]} {
		if err := processor.ApplyCardOutcomeEdit(f.ctx, bot, payload); err != nil {
			t.Fatal(err)
		}
	}
	calls := fake.recorded()
	if len(calls) != 1 || !strings.Contains(fmt.Sprint(calls[0].Body["text"]), "hanya dipakai") {
		t.Fatalf("telegram calls=%+v; only the newest outcome may be applied", calls)
	}
}

// A repeated tap on a card that is asking the remember question does not
// retire it: the question is live after its request resolved. A button
// retirement queued before the card's first outcome is dropped once the
// outcome exists, so out-of-order delivery cannot erase the question.
func TestDuplicateTapKeepsTheRememberQuestion(t *testing.T) {
	f := newCardFixture(t)
	category := f.id(`INSERT INTO category(household_id,name,slug) VALUES($1,'Galon Isi Ulang','galon') RETURNING id`, f.household)
	merchant := f.id(`INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Tins Mart') RETURNING id`, f.household)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',23000,now(),$2) RETURNING id`, f.household, merchant)
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, transaction)
	request := f.bindCard(item, transaction, "AMBIGUOUS_CATEGORY", "AWAITING_CATEGORY", 95, "card")

	// A retirement queued while the card was still a plain question.
	update := callbackUpdate(f.chat, 95, "review:edit")
	if err := enqueueRetireButtons(withCallbackQuestion(f.ctx, `{"callback_query":{"message":{"text":"card"}}}`), f.pool, update, "Tidak lagi berlaku."); err != nil {
		t.Fatal(err)
	}
	var early string
	f.row(`SELECT payload_json::text FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=95 ORDER BY created_at,id LIMIT 1`, []any{&early}, f.chat)
	earlyPayload, err := DecodeEditPayload(json.RawMessage(early))
	if err != nil || earlyPayload.OutcomeRequestID != request || earlyPayload.OutcomeRevision != 0 {
		t.Fatalf("early retirement payload=%s err=%v", early, err)
	}

	f.tap(95, "review:cat:"+category, "card")
	question, _ := f.lastEdit(95)
	edits := f.count(`SELECT count(*) FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=95`, f.chat)
	// A duplicate category tap and a stale detail tap on the question card.
	f.tap(95, "review:cat:"+category, question)
	f.tap(95, "review:edit", question)
	if again := f.count(`SELECT count(*) FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=95`, f.chat); again != edits {
		t.Fatalf("a duplicate tap queued %d edits that would erase the remember question", again-edits)
	}
	var state string
	f.row(`SELECT state FROM review_conversation WHERE review_request_id=$1`, []any{&state}, request)
	if state != "AWAITING_MERCHANT_DECISION" {
		t.Fatalf("conversation=%s", state)
	}
	// The early retirement delivered late is dropped.
	bot, fake := newFakeTelegram(t, nil)
	if err := NewProcessor(f.pool, nil).ApplyCardOutcomeEdit(f.ctx, bot, earlyPayload); err != nil {
		t.Fatal(err)
	}
	if calls := fake.recorded(); len(calls) != 0 {
		t.Fatalf("a late retirement erased the question: %+v", calls)
	}
	f.tap(95, "review:remember", question)
	if text, markup := f.lastEdit(95); !strings.Contains(text, "Diingat untuk Tins Mart") || strings.Contains(markup, "review:") {
		t.Fatalf("remember after duplicate taps: %q %s", text, markup)
	}
}

// An incoming duplicate that also needs its transfer relationship shows the
// income/transfer choices its instruction names, not Benar/Ubah.
func TestIncomingTransferCardButtonsMatchItsInstruction(t *testing.T) {
	f := newCardFixture(t)
	transaction := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','NEEDS_REVIEW',300000,now()) RETURNING id`, f.household)
	decision := `{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","subject":{"type":"transaction","id":"x"},"decisionClass":"DUPLICATE_AMBIGUITY","missingFacts":["duplicate_relationship","transfer_relationship"],"allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"],"interactionMode":"BOUNDED_CHOICE"}`
	item := f.id(`INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb) RETURNING id`, f.household, transaction, decision)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProjectReviewItem(f.ctx, tx, f.household, item, 0, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var text, markup string
	f.row(`SELECT payload_json->>'text',payload_json->'reply_markup'::text FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id=$1)`, []any{&text, &markup}, item)
	if !strings.Contains(text, "Pilih Penghasilan") || !strings.Contains(markup, "review:income") || !strings.Contains(markup, "review:own") || strings.Contains(markup, "review:confirm") {
		t.Fatalf("incoming transfer card: %q %s", text, markup)
	}
}
