package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

var interactionMessageSeq atomic.Int64

func nextMessageID() int64 { return 20000 + interactionMessageSeq.Add(1) }

func seedText(t *testing.T, ctx context.Context, f agentIntegrationFixture, text string, messageID, replyTo int64) string {
	t.Helper()
	update := f.update
	update.Message.Text = text
	update.Message.MessageID = messageID
	if replyTo != 0 {
		update.Message.ReplyToMessage = replyToMessage(replyTo)
	}
	raw, err := json.Marshal(update)
	mustAgentTest(t, err)
	stamp := testsupport.UniqueStamp()
	var sourceID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED',$4,$5) RETURNING id`,
		f.householdID, fmt.Sprintf("seed-%d-%d", stamp, messageID), []byte(fmt.Sprintf("seed-%d-%d", stamp, messageID)), messageID, f.chatID).Scan(&sourceID))
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw))
	mustAgentTest(t, err)
	return sourceID
}

func sendText(t *testing.T, ctx context.Context, f agentIntegrationFixture, p *Processor, text string, replyTo int64) (string, error) {
	t.Helper()
	sourceID := seedText(t, ctx, f, text, nextMessageID(), replyTo)
	return sourceID, p.ProcessAgent(ctx, sourceID)
}

func seedBotMessage(t *testing.T, ctx context.Context, f agentIntegrationFixture, text string, replyTo, messageID int64) {
	t.Helper()
	var jobID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO job(type,payload_json,status) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text),'SUCCEEDED') RETURNING id::text`, f.chatID, replyTo, text).Scan(&jobID))
	_, err := f.pool.Exec(ctx, `INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3,$4)`, jobID, f.householdID, f.chatID, messageID)
	mustAgentTest(t, err)
}

func deliverQueuedReply(t *testing.T, ctx context.Context, f agentIntegrationFixture, text string) int64 {
	t.Helper()
	messageID := nextMessageID()
	var jobID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id::text FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text'=$2 ORDER BY created_at DESC LIMIT 1`, f.chatID, text).Scan(&jobID))
	_, err := f.pool.Exec(ctx, `INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3,$4)`, jobID, f.householdID, f.chatID, messageID)
	mustAgentTest(t, err)
	return messageID
}

func chooseInteraction(t *testing.T, ctx context.Context, f agentIntegrationFixture, p *Processor, question, data string) {
	t.Helper()
	questionID := deliverQueuedReply(t, ctx, f, question)
	raw, err := json.Marshal(callbackUpdate(f.chatID, questionID, data))
	mustAgentTest(t, err)
	stamp := testsupport.UniqueStamp()
	var sourceID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`,
		f.householdID, fmt.Sprintf("choice-%d", stamp), []byte(fmt.Sprintf("choice-%d", stamp))).Scan(&sourceID))
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw))
	mustAgentTest(t, err)
	mustAgentTest(t, p.Process(ctx, sourceID))
}

func interactionEvents(t *testing.T, ctx context.Context, f agentIntegrationFixture, action string) []string {
	t.Helper()
	rows, err := f.pool.Query(ctx, `SELECT COALESCE(source_type,'') FROM product_telemetry_event WHERE household_id=$1 AND event_type='INTERACTION' AND action=$2 ORDER BY occurred_at,id`, f.householdID, action)
	mustAgentTest(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var source string
		mustAgentTest(t, rows.Scan(&source))
		out = append(out, source)
	}
	return out
}

func wantInteraction(t *testing.T, ctx context.Context, f agentIntegrationFixture, action string, want ...string) {
	t.Helper()
	got := interactionEvents(t, ctx, f, action)
	if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", want) {
		t.Fatalf("%s telemetry = %q, want %q", action, got, want)
	}
}

func createMerchantReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, amount string, messageID int64) (string, string) {
	t.Helper()
	reviewID, transactionID := createAgentTransactionReview(t, ctx, f, amount, messageID)
	_, err := f.pool.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_MERCHANT' WHERE review_request_id=$1`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE review_request SET review_type='UNKNOWN_MERCHANT' WHERE id=$1`, reviewID)
	mustAgentTest(t, err)
	return reviewID, transactionID
}

func requireBoundToCard(t *testing.T, model *capturingGateway) {
	t.Helper()
	if len(model.requests) == 0 {
		t.Fatal("the model was never called")
	}
	raw, err := json.Marshal(model.turnContext(t, 0)["open_review_cards"])
	mustAgentTest(t, err)
	if !strings.Contains(string(raw), `"card_ref":"k1"`) {
		t.Fatalf("the answer did not bind to the card: %s", raw)
	}
	names := model.toolNames(0)
	if !names[toolShowReviewCard] || names["resolve_review"] {
		t.Fatalf("the card step is not shown as a card: %v", names)
	}
}

func TestReplyToTheCardBindsAndRecordsTheStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-reply")
	seedCategory(t, ctx, f, "makan", "Makan")
	createAgentTransactionReview(t, ctx, f, "36500", 501)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), "makan", 501)
	mustAgentTest(t, err)
	requireBoundToCard(t, model)
	wantInteraction(t, ctx, f, interactionAnswered, "REPLY")
}

func TestReplyToABotFollowUpPromptBindsToItsCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-thread")
	seedCategory(t, ctx, f, "makan", "Makan")
	createMerchantReview(t, ctx, f, "36500", 501)
	seedBotMessage(t, ctx, f, "Balas pesan ini dengan akun kekayaan tujuan.", 501, 502)
	seedBotMessage(t, ctx, f, "Masih ada yang kurang.", 502, 503)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), "emas", 503)
	mustAgentTest(t, err)
	requireBoundToCard(t, model)
	wantInteraction(t, ctx, f, interactionAnswered, "THREAD_REPLY")
}

func TestReplyToTheUsersOwnEarlierAnswerBindsToTheCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-own-answer")
	createMerchantReview(t, ctx, f, "36500", 501)
	seedText(t, ctx, f, "kopi", 600, 501)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), "maksudku Kopi Kenangan", 600)
	mustAgentTest(t, err)
	requireBoundToCard(t, model)
	wantInteraction(t, ctx, f, interactionAnswered, "THREAD_REPLY")
}

func TestReplyChainStopsAtAMerchantLearningQuestionAndNeverStealsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-merchant-learning")
	learningID, _ := createMerchantLearningReview(t, ctx, f, 301, "resolvera")
	_, err := f.pool.Exec(ctx, `UPDATE review_request SET review_type='UNKNOWN_PURPOSE' WHERE id=$1`, learningID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE review_item SET review_type='UNKNOWN_PURPOSE' WHERE id=(SELECT review_item_id FROM review_request WHERE id=$1)`, learningID)
	mustAgentTest(t, err)
	createMerchantReview(t, ctx, f, "70000", 501)
	seedBotMessage(t, ctx, f, "Catatan tambahan.", 301, 302)
	model := &capturingGateway{}
	_, err = sendText(t, ctx, f, NewProcessor(f.pool, model), "ya", 302)
	mustAgentTest(t, err)
	if len(model.requests) == 0 || model.toolNames(0)["resolve_review"] {
		t.Fatal("a reply chained to a merchant-learning question bound to a review card")
	}
	model = &capturingGateway{}
	_, err = sendText(t, ctx, f, NewProcessor(f.pool, model), "ya", 301)
	mustAgentTest(t, err)
	for _, removed := range []string{"resolve_review", "resolve_merchant_learning"} {
		if len(model.requests) != 0 && model.toolNames(0)[removed] {
			t.Fatalf("the assistant was offered %s for a reply to the merchant-learning question", removed)
		}
	}
}

func TestReplyChainToAClosedCardGetsFixedCopyWithoutTheModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-closed")
	reviewID, _ := createMerchantReview(t, ctx, f, "36500", 501)
	_, err := f.pool.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1`, reviewID)
	mustAgentTest(t, err)
	seedBotMessage(t, ctx, f, "Balas pesan ini dengan akun kekayaan tujuan.", 501, 502)
	model := &capturingGateway{}
	_, err = sendText(t, ctx, f, NewProcessor(f.pool, model), "emas", 502)
	mustAgentTest(t, err)
	if len(model.requests) != 0 || lastReply(t, ctx, f) != closedCardReply {
		t.Fatalf("closed card: model calls=%d reply=%q", len(model.requests), lastReply(t, ctx, f))
	}
	wantInteraction(t, ctx, f, interactionClosedReply, "")
}

func TestReplyChainThatReachesNothingIsOrdinaryChat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-unmatched")
	createMerchantReview(t, ctx, f, "36500", 501)
	seedBotMessage(t, ctx, f, "Halo.", 9000, 9001)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), "berapa pengeluaran bulan ini?", 9001)
	mustAgentTest(t, err)
	if len(model.requests) == 0 || model.turnContext(t, 0)["workflow_scope"] != string(agentWorkflowGeneral) || model.toolNames(0)["resolve_review"] {
		t.Fatalf("an unbound reply was not ordinary chat: %v", model.requests)
	}
	wantInteraction(t, ctx, f, interactionUnmatched, "")
}

func TestPlainMessageBindsToTheSingleWaitingCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-plain")
	createMerchantReview(t, ctx, f, "36500", 501)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), "Warung Bu Siti", 0)
	mustAgentTest(t, err)
	requireBoundToCard(t, model)
	wantInteraction(t, ctx, f, interactionAnswered, "PLAIN")
}

func TestPlainMessageFittingTwoCardsAsksWhichOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-two-cards")
	seedCategory(t, ctx, f, "makan", "Makan")
	createAgentTransactionReview(t, ctx, f, "36500", 501)
	createAgentTransactionReview(t, ctx, f, "70000", 502)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), "makan", 0)
	mustAgentTest(t, err)
	if len(model.requests) != 0 || countRows(t, ctx, f, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text' LIKE '%Untuk yang mana%' AND (payload_json->>'chat_id')::bigint=$1`, f.chatID) != 1 {
		t.Fatalf("two matching cards must ask which one without the model (calls=%d)", len(model.requests))
	}
	wantInteraction(t, ctx, f, interactionAmbiguousEvent, "")
}

func TestAmountMessageBesideAMerchantStepAsksAnswerOrNew(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-answer-or-new")
	createMerchantReview(t, ctx, f, "36500", 501)
	p := NewProcessor(f.pool, nil)
	sourceID, err := sendText(t, ctx, f, p, "kopi kenangan 26rb", 0)
	mustAgentTest(t, err)
	if countRows(t, ctx, f, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text'=$1 AND (payload_json->>'chat_id')::bigint=$2 AND (payload_json->'reply_markup')::text LIKE '%interaction:answer%'`, answerOrNewQuestion, f.chatID) != 1 {
		t.Fatal("the answer-or-new question with both buttons was not sent")
	}

	model := &capturingGateway{}
	p = NewProcessor(f.pool, model)
	chooseInteraction(t, ctx, f, p, answerOrNewQuestion, callbackInteractionAnswer)
	mustAgentTest(t, p.ProcessAgent(ctx, sourceID))
	requireBoundToCard(t, model)
	wantInteraction(t, ctx, f, interactionAnswered, "PLAIN")
}

func TestAmountMessageChosenAsNewNoteSkipsTheCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-new-note")
	createMerchantReview(t, ctx, f, "36500", 501)
	p := NewProcessor(f.pool, nil)
	sourceID, err := sendText(t, ctx, f, p, "kopi kenangan 26rb", 0)
	mustAgentTest(t, err)
	model := &capturingGateway{}
	p = NewProcessor(f.pool, model)
	chooseInteraction(t, ctx, f, p, answerOrNewQuestion, callbackInteractionNew)
	mustAgentTest(t, p.ProcessAgent(ctx, sourceID))
	if len(model.requests) == 0 || model.toolNames(0)["resolve_review"] || !model.toolNames(0)["record_transaction"] {
		t.Fatalf("a new note must be ordinary chat: %v", model.requests)
	}
	wantInteraction(t, ctx, f, interactionAnswered)
}

func TestExactButtonLabelTypedActsAsTheTap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-label")
	reviewID, transactionID := createMerchantReview(t, ctx, f, "36500", 501)
	model := &capturingGateway{}
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, model), " abaikan ", 501)
	mustAgentTest(t, err)
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&status))
	if len(model.requests) != 0 || status != "VOIDED" || countRows(t, ctx, f, `SELECT count(*) FROM review_request WHERE id=$1 AND status='OPEN'`, reviewID) != 0 {
		t.Fatalf("typed Abaikan: model calls=%d transaction=%s", len(model.requests), status)
	}
	wantInteraction(t, ctx, f, interactionAnswered, "LABEL_TEXT")
}

func TestRepeatedInputWithinThreeMinutesIsRecorded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-repeated")
	model := &capturingGateway{}
	p := NewProcessor(f.pool, model)
	_, err := sendText(t, ctx, f, p, "Berapa total bulan ini", 0)
	mustAgentTest(t, err)
	wantInteraction(t, ctx, f, interactionRepeated)
	if _, flagged := model.turnContext(t, 0)["repeated_question"]; flagged {
		t.Fatal("a first question must not carry the repeated flag")
	}
	_, err = sendText(t, ctx, f, p, "  berapa  TOTAL bulan ini ", 0)
	mustAgentTest(t, err)
	wantInteraction(t, ctx, f, interactionRepeated, "")
	if model.turnContext(t, 1)["repeated_question"] != true {
		t.Fatal("the repeated question must reach the model context")
	}
}

func createPayslipCard(t *testing.T, ctx context.Context, f agentIntegrationFixture, messageID int64) (string, string) {
	t.Helper()
	stamp := testsupport.UniqueStamp()
	var imageID, attachmentID, documentID, proposalID, itemID, requestID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.householdID, fmt.Sprintf("slip-image-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&imageID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, f.householdID, []byte(fmt.Sprint(stamp)), fmt.Sprintf("%s/slip.png", f.householdID)).Scan(&attachmentID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'PAYSLIP','NEEDS_REVIEW') RETURNING id`, f.householdID, imageID, attachmentID).Scan(&documentID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,counterparty_raw,description,confidence,proposal_status,metadata_json) VALUES($1,$2,'INCOME',1000000,'IDR',now(),'Employer','Penghasilan dari slip gaji',.99,'NEEDS_REVIEW',jsonb_build_object('period','2026-09','document_id',$3::uuid)) RETURNING id`, f.householdID, imageID, documentID).Scan(&proposalID))
	decision := `{"version":1,"reasonCode":"MISSING_PAY_DATE","decisionClass":"HUMAN_POLICY_CHOICE","interactionMode":"POLICY_CHOICE","knownFacts":{},"missingFacts":["transaction_at","salary_classification"],"allowedActions":["SET_PAY_DATE","PRIMARY_SALARY","ORDINARY_INCOME","IGNORE"],"decisionSource":"DETERMINISTIC"}`
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,proposal_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,$4,'MISSING_PAY_DATE','OPEN',$5::jsonb) RETURNING id`, f.householdID, proposalID, imageID, documentID, decision).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,telegram_chat_id,status) VALUES($1,$2,'MISSING_PAY_DATE',$3,'OPEN') RETURNING id`, itemID, f.householdID, f.chatID).Scan(&requestID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, requestID, f.chatID, messageID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, requestID)
	mustAgentTest(t, err)
	return itemID, requestID
}

func TestReplayTypedSalaryLabelLandsOnThePayslipCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "replay-salary-label")
	itemID, requestID := createPayslipCard(t, ctx, f, 61)
	_, err := sendText(t, ctx, f, NewProcessor(f.pool, payslipDateGateway{}), "Gaji utama", 0)
	mustAgentTest(t, err)
	var state string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, requestID).Scan(&state))
	var choice string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(decision->'knownFacts'->>'salary_classification','') FROM review_item WHERE id=$1`, itemID).Scan(&choice))
	if state != "AWAITING_DATE" || choice == "" {
		t.Fatalf("typed salary label did not act as the tap: state=%s classification=%q", state, choice)
	}
	wantInteraction(t, ctx, f, interactionAnswered, "LABEL_TEXT")
}

func TestReplayPlainYesReachesTheSingleWaitingConfirmationCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "replay-plain-yes")
	var imageID, proposalID, itemID, requestID string
	stamp := testsupport.UniqueStamp()
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.householdID, fmt.Sprintf("yes-image-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&imageID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,confidence,proposal_status,metadata_json) VALUES($1,$2,'row-001','INCOME',NULL,'IDR','2026-10-03T12:00:00+07:00','Pengirim',0.9,'NEEDS_REVIEW','{"date_known":true,"row_index":0}'::jsonb) RETURNING id`, f.householdID, imageID).Scan(&proposalID))
	decision := `{"version":1,"reasonCode":"MISSING_AMOUNT","decisionClass":"EVIDENCE_GAP","knownFacts":{},"missingFacts":["amount","transfer_relationship"],"allowedActions":["CONFIRM_REVIEW","IGNORE"],"interactionMode":"SINGLE_FIELD"}`
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,proposal_id,source_event_id,review_type,status,decision) VALUES($1,$2,$3,'MISSING_AMOUNT','OPEN',$4::jsonb) RETURNING id`, f.householdID, proposalID, imageID, decision).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,telegram_chat_id,status) VALUES($1,$2,'MISSING_AMOUNT',$3,'OPEN') RETURNING id`, itemID, f.householdID, f.chatID).Scan(&requestID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,71)`, requestID, f.chatID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state,context_json) VALUES($1,'AWAITING_CONFIRMATION','{"amount_idr":"150000"}'::jsonb)`, requestID)
	mustAgentTest(t, err)
	model := &capturingGateway{}
	_, err = sendText(t, ctx, f, NewProcessor(f.pool, model), "ya", 0)
	mustAgentTest(t, err)
	if len(model.requests) != 0 {
		t.Fatal("a plain yes beside a single confirmation card went to the model instead of the card")
	}
	if countRows(t, ctx, f, `SELECT count(*) FROM review_item WHERE id=$1 AND status='OPEN'`, itemID) != 1 || countRows(t, ctx, f, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%dicatat sebagai apa%'`, f.chatID) != 1 {
		t.Fatal("the card did not answer its own confirmation step")
	}
	wantInteraction(t, ctx, f, interactionAnswered, "PLAIN")
}

func TestTapAndCardDeliveryAreRecordedAsInteractionSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-tap-step")
	reviewID, _ := createMerchantReview(t, ctx, f, "36500", 501)
	p := NewProcessor(f.pool, nil)
	mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 777, "Kartu baru"))
	if got := countRows(t, ctx, f, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND event_type='INTERACTION' AND action='STEP_SHOWN'`, f.householdID); got != 1 {
		t.Fatalf("STEP_SHOWN rows = %d, want 1", got)
	}
	tap := func(data string) {
		raw, err := json.Marshal(callbackUpdate(f.chatID, 777, data))
		mustAgentTest(t, err)
		var sourceID string
		mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, f.householdID, fmt.Sprintf("tap-%d", testsupport.UniqueStamp()), raw).Scan(&sourceID))
		_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw))
		mustAgentTest(t, err)
		mustAgentTest(t, p.Process(ctx, sourceID))
	}
	tap("review:catpage:1")
	wantInteraction(t, ctx, f, interactionAnswered)
	tap("review:ignore")
	wantInteraction(t, ctx, f, interactionAnswered, "TAP")
}

func TestReviewResolvedThroughTheAPIAfterATelegramCardIsRecordedAsFinishedOnWeb(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "resolver-web")
	reviewID, _ := createMerchantReview(t, ctx, f, "36500", 501)
	p := NewProcessor(f.pool, nil)
	mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 501, "Kartu"))
	resolve := func(applicationName string) {
		conn, err := f.pool.Acquire(ctx)
		mustAgentTest(t, err)
		defer conn.Release()
		_, err = conn.Exec(ctx, `SELECT set_config('application_name',$1,false)`, applicationName)
		mustAgentTest(t, err)
		defer conn.Exec(ctx, `SELECT set_config('application_name','',false)`)
		_, err = conn.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='IGNORE' WHERE id=(SELECT review_item_id FROM review_request WHERE id=$1)`, reviewID, f.userID)
		mustAgentTest(t, err)
	}
	resolve("richmod-api")
	wantInteraction(t, ctx, f, "FINISHED_ON_WEB", "")
	otherReview, _ := createMerchantReview(t, ctx, f, "40000", 601)
	mustAgentTest(t, p.BindReviewMessage(ctx, otherReview, f.chatID, 601, "Kartu"))
	reviewID = otherReview
	resolve("richmod-worker")
	wantInteraction(t, ctx, f, "FINISHED_ON_WEB", "")
}
