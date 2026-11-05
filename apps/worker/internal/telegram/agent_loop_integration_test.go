package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// Regression tests for the Telegram agent loop hardening (ADR-033/052/055
// amendments, 2026-11). Each names the finding it pins.

// lastReply is the newest conversational reply in the chat (review cards, which
// are their own projection, are left out).
func lastReply(t *testing.T, ctx context.Context, f agentIntegrationFixture) string {
	t.Helper()
	var text string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE((SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND NOT payload_json ? 'review_request_id' ORDER BY created_at DESC,id DESC LIMIT 1),'')`, f.chatID).Scan(&text))
	return text
}

func recordCall(id, arguments string) gateway.ToolCall {
	return gateway.ToolCall{CallID: id, Name: "record_transaction", Arguments: json.RawMessage(arguments)}
}

const kopiRecordArgs = `{"type":"EXPENSE","amount_idr":"26500","merchant":"Kopi Kenangan","category_slug":"makan","description":null,"note":null,"date_reference":"TODAY","date_provenance":"USER_STATED","explicit_date":null,"local_time":"15.00","ambiguous":false,"confidence":0.95,"category_confidence":0.9}`

func seedCategory(t *testing.T, ctx context.Context, f agentIntegrationFixture, slug, name string) string {
	t.Helper()
	var id string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, f.householdID, name, slug).Scan(&id))
	return id
}

// A5/A6/T7/A4: a clear record is one model call, the reply is deterministic and
// shows merchant, category and date, the reply and the ASSISTANT turn commit
// with the mutation, and the Indonesian "15.00" time is accepted.
func TestRecordTurnIsOneModelCallWithADeterministicDetailedReply(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-record")
	seedCategory(t, ctx, f, "makan", "Makan & Minum")
	model := &capturingGateway{script: []gateway.AgentResponse{{ToolCalls: []gateway.ToolCall{recordCall("r1", kopiRecordArgs)}}}}
	sourceID, err := runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "kopi kenangan 26.500 jam 15.00", 0)
	mustAgentTest(t, err)
	if len(model.requests) != 1 {
		t.Fatalf("a fully known mutation needs one model call, got %d", len(model.requests))
	}
	reply := lastReply(t, ctx, f)
	for _, want := range []string{"✅", "Kopi Kenangan", "Rp26.500", "Makan & Minum", "15:00"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply %q is missing %q", reply, want)
		}
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM telegram_conversation_turn WHERE source_event_id=$1 AND role='ASSISTANT' AND message_text=$2`, sourceID, reply); n != 1 {
		t.Fatalf("the reply must be stored once as the ASSISTANT turn, got %d", n)
	}
	// Item 13 / merchant learning: the typed merchant got the household identity.
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction t JOIN merchant m ON m.id=t.merchant_id WHERE t.household_id=$1 AND m.normalized_name='Kopi Kenangan'`, f.householdID); n != 1 {
		t.Fatalf("the typed merchant was not resolved to a merchant row (%d)", n)
	}
}

// A6: a transaction left for review points at the card sent to this chat, not
// at the Inbox.
func TestNeedsReviewReplyPointsAtTheCardBelow(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-review-reply")
	args := strings.Replace(kopiRecordArgs, `"category_slug":"makan"`, `"category_slug":null`, 1)
	model := &capturingGateway{script: []gateway.AgentResponse{{ToolCalls: []gateway.ToolCall{recordCall("r1", args)}}}}
	_, err := runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "kopi kenangan 26.500", 0)
	mustAgentTest(t, err)
	var reply string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%Kucatat dulu%'`, f.chatID).Scan(&reply))
	if strings.Contains(reply, "Kotak Tinjauan") || !strings.Contains(reply, "kartu di bawah") || strings.Contains(reply, "✅") {
		t.Fatalf("needs-review reply = %q", reply)
	}
}

// A2/A1: a failure reply is stored as a marked ASSISTANT turn, bookkeeping ref
// rows never reach the model, and the memory window counts user exchanges.
func TestMemoryKeepsFailuresAndSkipsBookkeepingRows(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-memory")
	p := NewProcessor(f.pool, nil)
	state := *f.state
	mustAgentTest(t, p.finishAgentFailure(ctx, &state, "Aku belum bisa memproses permintaan ini.", "UNSAFE_TOOL_CALL"))
	for index := 0; index < 8; index++ {
		mustAgentTest(t, p.persistTurn(ctx, f.householdID, "", f.update, "TOOL", "Agent transaction references.", "agent_transaction_refs", map[string]any{"transaction_refs": []string{"x"}}))
	}
	for index := 0; index < 4; index++ {
		mustAgentTest(t, p.persistTurn(ctx, f.householdID, "", f.update, "USER", fmt.Sprintf("pertanyaan %d", index), "", nil))
		mustAgentTest(t, p.persistTurn(ctx, f.householdID, "", f.update, "TOOL", "", "query_spending", map[string]any{"net_expense_idr": "1000"}))
		mustAgentTest(t, p.persistTurn(ctx, f.householdID, "", f.update, "ASSISTANT", fmt.Sprintf("jawaban %d", index), "", nil))
	}
	turns, err := p.recentConversation(ctx, f.householdID, f.chatID, "00000000-0000-4000-8000-000000000001")
	mustAgentTest(t, err)
	whole, failures := 0, 0
	for _, turn := range turns {
		if turn.Tool == "agent_transaction_refs" {
			t.Fatal("a bookkeeping ref row reached model memory")
		}
		if turn.Role == "USER" && !turn.Compacted {
			whole++
		}
		if turn.Failure == "UNSAFE_TOOL_CALL" {
			failures++
		}
	}
	if whole != verbatimExchanges || failures != 1 {
		t.Fatalf("whole user exchanges=%d (want %d), failure turns=%d (want 1): %+v", whole, verbatimExchanges, failures, turns)
	}
}

// A3: with the gateway unreachable, a simple expense is still recorded for
// review deterministically, with the review card, never auto-confirmed.
func TestGatewayDownRecordsASimpleExpenseForReview(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-fallback")
	model := &capturingGateway{err: &gateway.UnavailableError{Err: errors.New("dial tcp: connection refused")}}
	sourceID, err := runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "kopi 25rb", 0)
	mustAgentTest(t, err)
	var status, description, amount, sourceStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.description,''),t.amount::text,s.processing_status FROM transaction t JOIN transaction_evidence te ON te.transaction_id=t.id JOIN source_event s ON s.id=te.source_event_id WHERE s.id=$1`, sourceID).Scan(&status, &description, &amount, &sourceStatus))
	if status != "NEEDS_REVIEW" || description != "kopi" || amount != "25000" || sourceStatus != "NEEDS_REVIEW" {
		t.Fatalf("fallback recorded status=%s description=%q amount=%s source=%s", status, description, amount, sourceStatus)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM review_request r JOIN transaction_evidence te ON te.transaction_id=r.transaction_id WHERE te.source_event_id=$1 AND r.telegram_chat_id=$2`, sourceID, f.chatID); n != 1 {
		t.Fatalf("the fallback must send the normal review card, got %d", n)
	}
	var reply string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(max(payload_json->>'text'),'') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%tidak bisa dihubungi%'`, f.chatID).Scan(&reply))
	if !strings.Contains(reply, "Rp25.000") || !strings.Contains(reply, "kopi") || strings.Contains(reply, "✅") {
		t.Fatalf("fallback reply = %q", reply)
	}

	// Anything that is not a plain expense fails honestly and records nothing.
	before := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID)
	_, err = runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "berapa pengeluaran bulan ini?", 0)
	if !IsGatewayUnavailable(err) {
		t.Fatalf("a question with the gateway down must fail as unavailable, got %v", err)
	}
	if after := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID); after != before {
		t.Fatal("the fallback recorded a non-expense message")
	}
}

func TestParseSimpleExpense(t *testing.T) {
	for _, tc := range []struct {
		text, amount, description string
	}{
		{"kopi 25rb", "25000", "kopi"}, {"bensin 50.000", "50000", "bensin"}, {"parkir 5k", "5000", "parkir"},
		{"Rp 75.000 makan siang", "", ""}, // "siang" is a time of day: not a plain expense
		{"beli nasi goreng 18 ribu", "18000", "nasi goreng"}, {"pulsa 1,5jt", "1500000", "pulsa"}, {"rp25.000 sabun", "25000", "sabun"},
		{"kopi 25", "", ""}, {"gaji 5jt", "", ""}, {"kopi 25rb kemarin", "", ""}, {"berapa kopi 25rb?", "", ""},
		{"kopi 25rb dan roti 10rb", "", ""}, {"25rb", "", ""}, {"tf 50rb ke ibu", "", ""}, {"bensin 50.00", "", ""},
	} {
		got, ok := parseSimpleExpense(tc.text)
		if tc.amount == "" {
			if ok {
				t.Fatalf("%q must not parse, got %+v", tc.text, got)
			}
			continue
		}
		if !ok || got.Amount != tc.amount || got.Description != tc.description {
			t.Fatalf("%q = %+v ok=%v, want %s/%s", tc.text, got, ok, tc.amount, tc.description)
		}
	}
}

// A4: periods Go cannot serve are tool results the model can act on, never a
// failed job.
func TestUnservablePeriodsAreRecoverableReadResults(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-periods")
	p := NewProcessor(f.pool, nil)
	for _, tc := range []struct {
		args   map[string]any
		status string
	}{
		{map[string]any{"period": "CURRENT_CYCLE"}, "CYCLE_UNAVAILABLE"},
		{map[string]any{"period": "CUSTOM", "from_date": "2020-01-01", "to_date": "2026-01-01"}, "RANGE_OUT_OF_BOUNDS"},
		{map[string]any{"period": "CUSTOM", "from_date": "kemarin", "to_date": "2026-01-01"}, "INVALID_RANGE"},
	} {
		result, err := p.executeAgentRead(ctx, f.state, gateway.ToolCall{Name: "query_spending"}, tc.args, "fixture")
		if err != nil || result.Status != tc.status || result.Facts["next_step"] == nil {
			t.Fatalf("%v: status=%s err=%v facts=%v", tc.args, result.Status, err, result.Facts)
		}
	}
}

// A4: date/time shape errors are argument errors (ADR-055 repair), not job errors.
func TestValidateCreateCoversDateAndTimeRules(t *testing.T) {
	base := createArgs{Type: "EXPENSE", Amount: "1000"}
	ok := base
	ok.LocalTime = strPtr("15.00")
	if err := validateCreate(ok); err != nil {
		t.Fatalf("15.00 is a valid local time: %v", err)
	}
	for name, mutate := range map[string]func(*createArgs){
		"bad local time":       func(a *createArgs) { a.LocalTime = strPtr("jam tiga") },
		"missing provenance":   func(a *createArgs) { a.DateReference = "TODAY" },
		"explicit without day": func(a *createArgs) { a.DateReference, a.DateProvenance = "EXPLICIT", strPtr("USER_STATED") },
		"bad explicit date": func(a *createArgs) {
			a.DateReference, a.DateProvenance, a.ExplicitDate = "EXPLICIT", strPtr("USER_STATED"), strPtr("3 Okt")
		},
	} {
		args := base
		mutate(&args)
		if err := validateCreate(args); err == nil {
			t.Fatalf("%s must be rejected by validateCreate", name)
		}
	}
}

// A7: amount and merchant corrections apply only to self-entered transactions,
// through the confirmation token, audited; source-backed ones are refused.
func TestCorrectionsChangeAmountOnlyForSelfEnteredTransactions(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-correct")
	var selfTx, bankTx, bankSource string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',25000,'IDR',now(),'Warung Ibu',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&selfTx))
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TELEGRAM_TEXT')`, selfTx, f.sourceID)
	mustAgentTest(t, err)
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',90000,'IDR',now(),'Toko Bank',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&bankTx))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'PROCESSED') RETURNING id`, f.householdID, fmt.Sprintf("bank-%d", time.Now().UnixNano()), []byte("bank")).Scan(&bankSource))
	_, err = f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, bankTx, bankSource)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)

	refused, _, err := p.agentStageCorrection(ctx, f.state, gateway.ToolCall{}, map[string]any{"search_text": "Toko Bank", "period": "TODAY", "amount_idr": "95000"})
	mustAgentTest(t, err)
	if refused.Status != "SOURCE_EVIDENCE_FIXES_AMOUNT" || countRows(t, ctx, f, `SELECT count(*) FROM telegram_pending_action WHERE transaction_id=$1`, bankTx) != 0 {
		t.Fatalf("a bank-backed amount change must be refused before staging: %#v", refused)
	}

	staged, _, err := p.agentStageCorrection(ctx, f.state, gateway.ToolCall{}, map[string]any{"search_text": "Warung Ibu", "period": "TODAY", "amount_idr": "27000", "merchant": "Warung Bu Siti"})
	mustAgentTest(t, err)
	if staged.Status != "PENDING_CONFIRMATION" || staged.confirmationToken == "" {
		t.Fatalf("self-entered correction was not staged: %#v", staged)
	}
	var minutes float64
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT extract(epoch FROM expires_at-now())/60 FROM telegram_pending_action WHERE transaction_id=$1 AND status='PENDING'`, selfTx).Scan(&minutes))
	if minutes < 29 || minutes > 31 {
		t.Fatalf("pending correction must wait 30 minutes, got %.1f", minutes)
	}
	if reply := lastReply(t, ctx, f); !strings.Contains(reply, "nominal → Rp27.000") || !strings.Contains(reply, "merchant → Warung Bu Siti") {
		t.Fatalf("staged correction reply = %q", reply)
	}
	handled, err := p.processPendingEdit(ctx, f.householdID, f.update, f.sourceID, true, staged.confirmationToken)
	mustAgentTest(t, err)
	var amount, merchant string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT amount::text,counterparty_name FROM transaction WHERE id=$1`, selfTx).Scan(&amount, &merchant))
	if !handled || amount != "27000" || merchant != "Warung Bu Siti" {
		t.Fatalf("confirmed correction: handled=%v amount=%s merchant=%s", handled, amount, merchant)
	}
	var before, after string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT before_json->>'amount',after_json->>'amount' FROM audit_log WHERE entity_id=$1 AND action='EDIT_TRANSACTION'`, selfTx).Scan(&before, &after))
	if before != "25000" || after != "27000" {
		t.Fatalf("audit before/after = %s/%s", before, after)
	}
}

// A8a: an unrelated question while a batch is pending is a normal turn: the
// model answers it and the batch stays pending, with one merged batch tool.
func TestPendingBatchDoesNotSwallowAnUnrelatedQuestion(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-batch-unrelated")
	_, err := f.pool.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status) VALUES($1,$2,$2,$3,'[{"Type":"EXPENSE","Amount":"1000","Merchant":"A","CategorySlug":"","Description":"","Note":"","TransactionAt":"2026-11-01T10:00:00+07:00"}]'::jsonb,'PENDING')`, f.householdID, f.chatID, f.sourceID)
	mustAgentTest(t, err)
	model := &capturingGateway{script: []gateway.AgentResponse{{Text: "Pengeluaran bulan ini Rp0."}}}
	_, err = runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "berapa pengeluaran bulan ini?", 0)
	mustAgentTest(t, err)
	tools := model.toolNames(0)
	if !tools["pending_batch_decision"] || !tools["record_transaction"] || tools["confirm_pending_batch"] || tools["update_pending_batch"] {
		t.Fatalf("pending-batch turn catalog = %v", tools)
	}
	if lastReply(t, ctx, f) != "Pengeluaran bulan ini Rp0." || countRows(t, ctx, f, `SELECT count(*) FROM telegram_pending_batch WHERE household_id=$1 AND status='PENDING'`, f.householdID) != 1 {
		t.Fatal("the unrelated question was not answered or the batch was touched")
	}
}

// A8b: a reply to an ordinary bot message gets the general catalog; a reply to
// a closed review card stays narrowed and the model is told why.
func TestExplicitReplyToANonWorkflowMessageIsAnOrdinaryTurn(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-reply-general")
	model := &capturingGateway{}
	_, err := runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "kopi 20rb", 777)
	mustAgentTest(t, err)
	if turn := model.turnContext(t, 0); turn["workflow_scope"] != string(agentWorkflowGeneral) || turn["explicit_reply_unbound"] != nil || !model.toolNames(0)["record_transaction"] {
		t.Fatalf("reply to a plain bot message: scope=%v unbound=%v tools=%v", turn["workflow_scope"], turn["explicit_reply_unbound"], model.toolNames(0))
	}

	reviewID, _ := createAgentTransactionReview(t, ctx, f, "36500", 888)
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, reviewID)
	mustAgentTest(t, err)
	model = &capturingGateway{}
	_, err = runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "makan", 888)
	mustAgentTest(t, err)
	if turn := model.turnContext(t, 0); turn["explicit_reply_unbound"] != true || model.toolNames(0)["record_transaction"] {
		t.Fatalf("reply to a closed card: unbound=%v tools=%v", turn["explicit_reply_unbound"], model.toolNames(0))
	}
}

// R5: binding a typed answer is deterministic. One category card plus a category
// name binds; an expense text does not; two category cards ask with buttons
// and the tap resolves the chosen card without any model call; expired cards
// do not count.
func TestTypedAnswersBindToCardsOnlyByDeterministicRules(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-cards")
	seedCategory(t, ctx, f, "makan", "Makan")
	first, firstTx := createAgentTransactionReview(t, ctx, f, "36500", 501)

	model := &capturingGateway{}
	_, err := runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "kopi 20rb", 0)
	mustAgentTest(t, err)
	if model.toolNames(0)["resolve_review"] || model.turnContext(t, 0)["active_review"] != nil || model.turnContext(t, 0)["open_review_cards"] == nil {
		t.Fatalf("an expense text bound to the open card: %v", model.turnContext(t, 0))
	}
	model = &capturingGateway{}
	_, err = runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "masukin ke makan aja", 0)
	mustAgentTest(t, err)
	if !model.toolNames(0)["resolve_review"] || model.turnContext(t, 0)["active_review"] == nil {
		t.Fatal("a category answer to the single category card did not bind")
	}

	second, _ := createAgentTransactionReview(t, ctx, f, "70000", 502)
	model = &capturingGateway{}
	_, err = runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "makan", 0)
	mustAgentTest(t, err)
	if len(model.requests) != 0 {
		t.Fatal("an answer fitting two cards must not reach the model")
	}
	var markup string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->'reply_markup'::text FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%Untuk yang mana%' ORDER BY created_at DESC LIMIT 1`, f.chatID).Scan(&markup))
	var token string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT o.token::text FROM telegram_pending_card_choice_option o JOIN telegram_pending_card_choice c ON c.id=o.choice_id WHERE c.household_id=$1 AND o.review_request_id=$2`, f.householdID, first).Scan(&token))
	if !strings.Contains(markup, callbackPendingCardPrefix+token) || !strings.Contains(markup, "Rp36.500") || !strings.Contains(markup, "Rp70.000") {
		t.Fatalf("pick buttons = %s", markup)
	}
	tap := f.update
	tap.Message.MessageID = 9999
	mustAgentTest(t, func() error {
		_, err := NewProcessor(f.pool, nil).processCardChoiceCallback(ctx, f.sourceID, f.householdID, tap, callbackPendingCardPrefix+token)
		return err
	}())
	var status, slug string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(c.slug,'') FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.id=$1`, firstTx).Scan(&status, &slug))
	if status != "CONFIRMED" || slug != "makan" {
		t.Fatalf("the picked card was not resolved: status=%s category=%s", status, slug)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM review_request WHERE id=$1 AND status='OPEN'`, second); n != 1 {
		t.Fatal("the other card must stay open")
	}

	_, err = f.pool.Exec(ctx, `UPDATE review_request SET expires_at=now()-interval '1 minute' WHERE id=$1`, second)
	mustAgentTest(t, err)
	cards, err := NewProcessor(f.pool, nil).loadLiveReviewCards(ctx, f.householdID, f.chatID)
	mustAgentTest(t, err)
	if len(cards) != 0 {
		t.Fatalf("expired cards must not count, got %d", len(cards))
	}
}

// T7: a turn whose mutation committed but whose answer was lost is answered on
// retry, once, from the stored side-effect result.
func TestRetryOfACommittedTurnReSendsTheLostAnswerOnce(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-recover")
	seedCategory(t, ctx, f, "makan", "Makan & Minum")
	model := &capturingGateway{script: []gateway.AgentResponse{{ToolCalls: []gateway.ToolCall{recordCall("r1", kopiRecordArgs)}}}}
	p := NewProcessor(f.pool, model)
	sourceID, err := runEvidenceTurn(t, ctx, f, p, "kopi kenangan 26.500", 0)
	mustAgentTest(t, err)
	// Simulate the reply being lost after the commit.
	_, err = f.pool.Exec(ctx, `DELETE FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%Tercatat%'`, f.chatID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `DELETE FROM telegram_conversation_turn WHERE source_event_id=$1 AND role='ASSISTANT'`, sourceID)
	mustAgentTest(t, err)
	for attempt := 0; attempt < 2; attempt++ {
		mustAgentTest(t, p.ProcessAgent(ctx, sourceID))
	}
	if len(model.requests) != 1 {
		t.Fatal("recovery must not call the model again")
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%Tercatat: Kopi Kenangan%'`, f.chatID); n != 1 {
		t.Fatalf("the lost answer must be re-sent exactly once, got %d", n)
	}
}

// Item 13: a confirmed batch records categorized items, routes a category-less
// item to review with a card, shows a likely duplicate before confirmation, and
// resolves merchants.
func TestBatchLaneFollowsTheSingleEntryRules(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-batch")
	seedCategory(t, ctx, f, "makan", "Makan")
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',15000,'IDR',now()-interval '2 days','Bakso Pak Kumis',$2,now())`, f.householdID, f.userID)
	mustAgentTest(t, err)
	item := func(amount, merchant, category string) map[string]any {
		var slug any
		if category != "" {
			slug = category
		}
		return map[string]any{"type": "EXPENSE", "amount_idr": amount, "merchant": merchant, "category_slug": slug, "description": nil, "note": nil,
			"date_reference": "TODAY", "date_provenance": "USER_STATED", "explicit_date": nil, "local_time": "12:00", "ambiguous": false, "confidence": 1.0, "category_confidence": 1.0}
	}
	p := NewProcessor(f.pool, nil)
	staged, _, err := p.agentStageBatch(ctx, f.state, gateway.ToolCall{Name: "record_transaction_batch"}, map[string]any{"items": []any{
		item("20000", "Warteg", "makan"), item("15000", "Bakso Pak Kumis", "makan"), item("8000", "Fotokopi", ""),
	}})
	mustAgentTest(t, err)
	if reply := lastReply(t, ctx, f); !strings.Contains(reply, "mirip yang sudah tercatat") || !strings.Contains(reply, "kategori belum ada") {
		t.Fatalf("staged list must show the duplicate and the missing category: %q", reply)
	}
	state := *f.state
	confirmed, _, err := p.agentFinalizePendingBatch(ctx, &state, gateway.ToolCall{Name: "pending_batch_decision"}, true, nil)
	mustAgentTest(t, err)
	if confirmed.Mutation["recorded"] != 2 || confirmed.Mutation["needs_review"] != 1 {
		t.Fatalf("batch outcome = %#v (staged %#v)", confirmed.Mutation, staged.Mutation)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction t JOIN review_request r ON r.transaction_id=t.id WHERE t.household_id=$1 AND t.counterparty_name='Fotokopi' AND t.status='NEEDS_REVIEW'`, f.householdID); n != 1 {
		t.Fatal("the category-less item must be recorded for review with a card")
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND counterparty_name='Warteg' AND merchant_id IS NOT NULL`, f.householdID); n != 1 {
		t.Fatal("batch merchants must be resolved to merchant rows")
	}
	if reply := lastReply(t, ctx, f); !strings.Contains(reply, "2 transaksi sudah tercatat") || !strings.Contains(reply, "1 lagi belum punya kategori") {
		t.Fatalf("batch outcome reply = %q", reply)
	}
}

// Item 14: the resolve_review residual audit statement prepares with every
// parameter typed. The old statement fails exactly as production did.
func TestCycleResidualAuditStatementIsTyped(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-42p18")
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SAVEPOINT old_statement`)
	mustAgentTest(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,$3,'cycle_residual_case',$4,jsonb_build_object('residualIdr',$5,'agent_sprint',1))`, f.householdID, f.userID, "CYCLE_RESIDUAL_LEAVE_UNALLOCATED", f.sourceID, "125000")
	if err == nil || !strings.Contains(err.Error(), "42P18") {
		t.Fatalf("the untyped statement should reproduce 42P18, got %v", err)
	}
	_, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT old_statement`)
	mustAgentTest(t, err)
	if _, err := tx.Exec(ctx, cycleResidualAuditSQL, f.householdID, f.userID, "CYCLE_RESIDUAL_LEAVE_UNALLOCATED", f.sourceID, "125000"); err != nil {
		t.Fatalf("typed residual audit failed: %v", err)
	}
}

// Item 16: an identical repeated read runs once; the final phase answers from
// what was gathered instead of a fixed "one part at a time" refusal.
func TestRepeatedReadsRunOnceAndTheLastPhaseAnswers(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-reads")
	read := gateway.ToolCall{Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)}
	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if len(request.Tools) == 0 {
			return gateway.AgentResponse{Text: "Totalnya Rp0 sejauh ini."}
		}
		read.CallID = fmt.Sprintf("read-%d", call)
		return gateway.AgentResponse{ResponseID: fmt.Sprintf("resp-%d", call), ToolCalls: []gateway.ToolCall{read}}
	}}
	sourceID, err := runEvidenceTurn(t, ctx, f, NewProcessor(f.pool, model), "pengeluaran bulan ini berapa", 0)
	mustAgentTest(t, err)
	if len(model.requests) != defaultAgentLimits.MaxModelPhases || len(model.requests[len(model.requests)-1].Tools) != 0 {
		t.Fatalf("the last phase must be offered no tools; calls=%d", len(model.requests))
	}
	if lastReply(t, ctx, f) != "Totalnya Rp0 sejauh ini." {
		t.Fatalf("final-phase answer was not sent: %q", lastReply(t, ctx, f))
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM telegram_conversation_turn WHERE source_event_id=$1 AND role='TOOL' AND tool_name='query_spending'`, sourceID); n != 1 {
		t.Fatalf("an identical repeated read must run once, stored %d times", n)
	}
}

// Item 16: recent transactions list the member's own entries first.
func TestRecentTransactionsPreferTheMembersOwnEntries(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "loop-recent")
	var otherUser string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Other','unused') RETURNING id`, fmt.Sprintf("other-%d@example.test", time.Now().UnixNano())).Scan(&otherUser))
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,created_at,confirmed_at) VALUES
		($1,'EXPENSE','CONFIRMED',1000,'IDR',now(),'mine',$2,now()-interval '5 minutes',now()),
		($1,'EXPENSE','CONFIRMED',2000,'IDR',now(),'theirs',$3,now(),now())`, f.householdID, f.userID, otherUser)
	mustAgentTest(t, err)
	recent, err := NewProcessor(f.pool, nil).recentAgentTransactions(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if len(recent) != 2 || recent[0]["entered_by_you"] != true || recent[1]["entered_by_you"] != false {
		t.Fatalf("recent transactions = %+v", recent)
	}
}

func TestProgressNoticeNamesWhatTheTurnIsDoing(t *testing.T) {
	if progressNoticeFor(activityRecording) == progressNoticeFor(activityAnalyzing) || strings.Contains(progressNoticeFor(activityRecording), "analisis") || strings.Contains(progressNoticeFor(activityThinking), "analisis") {
		t.Fatal("a recording or thinking turn must not be described as an analysis")
	}
	for _, text := range progressNoticeTexts {
		if !strings.Contains(text, "menyusul") {
			t.Fatalf("every notice says the answer is coming: %q", text)
		}
	}
}
