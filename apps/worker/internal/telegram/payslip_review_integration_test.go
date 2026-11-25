package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestTelegramPayslipPolicyAndDateResolveWithoutWeb(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	chatID := stamp
	var householdID, userID, imageID, attachmentID, documentID, proposalID, itemID, requestID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Telegram payslip %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("payslip-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("payslip-image-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&imageID))
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, householdID, []byte(fmt.Sprint(stamp)), fmt.Sprintf("%s/payslip.png", householdID)).Scan(&attachmentID))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'PAYSLIP','NEEDS_REVIEW') RETURNING id`, householdID, imageID, attachmentID).Scan(&documentID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,counterparty_raw,description,confidence,proposal_status,metadata_json) VALUES($1,$2,'INCOME',1000000,'IDR',now(),'Employer','Penghasilan dari slip gaji',.99,'NEEDS_REVIEW',jsonb_build_object('period','2026-09','document_id',$3::uuid)) RETURNING id`, householdID, imageID, documentID).Scan(&proposalID))
	decision := `{"version":1,"reasonCode":"MISSING_PAY_DATE","decisionClass":"HUMAN_POLICY_CHOICE","interactionMode":"POLICY_CHOICE","knownFacts":{},"missingFacts":["transaction_at","salary_classification"],"allowedActions":["SET_PAY_DATE","PRIMARY_SALARY","ORDINARY_INCOME","IGNORE"],"decisionSource":"DETERMINISTIC"}`
	var contract reviewdec.Decision
	must(json.Unmarshal([]byte(decision), &contract))
	if card := buildReviewCard(contract, "MISSING_PAY_DATE", "Synthetic payslip", reviewSubject{}); card.State != "AWAITING_DETAIL" || card.Mode != "step" {
		t.Fatalf("policy must precede date: state=%s mode=%s", card.State, card.Mode)
	}
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,proposal_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,$4,'MISSING_PAY_DATE','OPEN',$5::jsonb) RETURNING id`, householdID, proposalID, imageID, documentID, decision).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,telegram_chat_id,status) VALUES($1,$2,'MISSING_PAY_DATE',$3,'OPEN') RETURNING id`, itemID, householdID, chatID).Scan(&requestID))
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,61)`, requestID, chatID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, requestID)
	must(err)
	seedReply := func(kind string, update telegramUpdate) string {
		raw, err := json.Marshal(update)
		must(err)
		var sourceID string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),$4,'RECEIVED') RETURNING id`, householdID, kind, fmt.Sprintf("payslip-%s-%d", kind, testsupport.UniqueStamp()), raw).Scan(&sourceID))
		_, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw))
		must(err)
		return sourceID
	}
	processor := NewProcessor(pool, payslipDateGateway{})
	callback := callbackUpdate(chatID, 61, "review:salary:primary")
	must(processor.Process(ctx, seedReply("TELEGRAM_CALLBACK", callback)))
	var state, requestStatus, itemStatus string
	must(pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, requestID).Scan(&state))
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&itemStatus))
	if state != "AWAITING_DATE" || itemStatus != "OPEN" {
		t.Fatalf("policy turn must leave date unresolved: state=%s item=%s", state, itemStatus)
	}
	dateReply := telegramUpdate{}
	dateReply.Message.MessageID, dateReply.Message.Chat.ID, dateReply.Message.From.ID = 62, chatID, chatID
	dateReply.Message.Text = "25 September 2026"
	dateReply.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 61}
	must(processor.ProcessAgent(ctx, seedReply("TELEGRAM_TEXT", dateReply)))
	var transactionID, amount, payDate, transactionStatus, proposalStatus string
	must(pool.QueryRow(ctx, `SELECT t.id::text,t.amount::text,(t.transaction_at AT TIME ZONE 'Asia/Jakarta')::date::text,t.status
		FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id AND e.source_event_id=$1 AND e.evidence_type='PAYSLIP_IMAGE'`, imageID).Scan(&transactionID, &amount, &payDate, &transactionStatus))
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&itemStatus))
	must(pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, requestID).Scan(&requestStatus))
	must(pool.QueryRow(ctx, `SELECT proposal_status FROM transaction_proposal WHERE id=$1`, proposalID).Scan(&proposalStatus))
	if amount != "1000000" || payDate != "2026-09-25" || transactionStatus != "CONFIRMED" || proposalStatus != "ACCEPTED" || itemStatus != "RESOLVED" || requestStatus != "RESOLVED" {
		t.Fatalf("payslip resolution txn=%s amount=%s date=%s status=%s proposal=%s item=%s request=%s", transactionID, amount, payDate, transactionStatus, proposalStatus, itemStatus, requestStatus)
	}
	var turns, choices, dates int
	must(pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(bounded_choices),0),count(*) FILTER(WHERE 'transaction_at'=ANY(changed_fields)) FROM product_telemetry_event WHERE review_item_id=$1 AND event_type='REVIEW_TURN'`, itemID).Scan(&turns, &choices, &dates))
	if turns != 2 || choices != 1 || dates != 1 {
		t.Fatalf("payslip turns=%d policy choices=%d dates=%d", turns, choices, dates)
	}
	must(processor.Process(ctx, seedReply("TELEGRAM_CALLBACK", callback)))
	var transactions int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction_evidence WHERE source_event_id=$1 AND evidence_type='PAYSLIP_IMAGE'`, imageID).Scan(&transactions))
	if transactions != 1 {
		t.Fatalf("stale callback created another payslip transaction: %d", transactions)
	}
	var residualJobs int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'household_id'=$1`, householdID).Scan(&residualJobs))
	if residualJobs != 1 {
		t.Fatalf("telegram payslip confirmation enqueued %d cycle residual jobs, want 1", residualJobs)
	}
}

type payslipDateGateway struct{}

func (payslipDateGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("payslip cards are answered without the model")
}

func TestQueuedReviewSendSkipsResolvedProjection(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var householdID, openItem, resolvedItem, expiredItem, openRequest, resolvedRequest, expiredRequest string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("queued send %d", stamp)).Scan(&householdID))
	newItem := func(status string, resolved bool) string {
		var sourceID, id string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("queued-%d-%s", testsupport.UniqueStamp(), status), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
		if resolved {
			must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY',$3,now(),'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID, status).Scan(&id))
		} else {
			must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY',$3,'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID, status).Scan(&id))
		}
		return id
	}
	resolvedItem = newItem("RESOLVED", true)
	openItem, expiredItem = newItem("OPEN", false), newItem("OPEN", false)
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, openItem, householdID).Scan(&openRequest))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status,resolved_at) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now()) RETURNING id`, resolvedItem, householdID).Scan(&resolvedRequest))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status,expires_at) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN',now()-interval '1 minute') RETURNING id`, expiredItem, householdID).Scan(&expiredRequest))
	processor := NewProcessor(pool, nil)
	if open, err := processor.ReviewProjectionOpen(ctx, openRequest); err != nil || !open {
		t.Fatalf("open projection must send: open=%v err=%v", open, err)
	}
	if open, err := processor.ReviewProjectionOpen(ctx, resolvedRequest); err != nil || open {
		t.Fatalf("resolved projection must be skipped: open=%v err=%v", open, err)
	}
	if open, err := processor.ReviewProjectionOpen(ctx, expiredRequest); err != nil || open {
		t.Fatalf("expired projection must be skipped: open=%v err=%v", open, err)
	}
	if open, err := processor.ReviewProjectionOpen(ctx, ""); err != nil || !open {
		t.Fatalf("empty review id must not block a non-review send: open=%v err=%v", open, err)
	}
}
