package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type payslipReviewWorld struct {
	pool                                           *pgxpool.Pool
	householdID                                    string
	chatID                                         int64
	imageID, documentID, proposalID, itemID, reqID string
}

// seedPayslipReview builds a payslip whose pay date is missing: the upload was
// message 55 in the chat, the review card is message 61, and the review is still
// waiting for an answer. It is the shape a CEU-06 reply must reach.
func seedPayslipReview(t *testing.T, ctx context.Context) payslipReviewWorld {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	w := payslipReviewWorld{pool: pool, chatID: stamp}
	var userID, attachmentID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Continuation %d", stamp)).Scan(&w.householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("cont-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, w.householdID, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, w.chatID, w.householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW',55,$4) RETURNING id`, w.householdID, fmt.Sprintf("cont-image-%d", stamp), []byte(fmt.Sprint(stamp)), w.chatID).Scan(&w.imageID))
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, w.householdID, []byte(fmt.Sprint(stamp)), fmt.Sprintf("cont/%d.png", stamp)).Scan(&attachmentID))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'PAYSLIP','NEEDS_REVIEW') RETURNING id`, w.householdID, w.imageID, attachmentID).Scan(&w.documentID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,counterparty_raw,description,confidence,proposal_status,metadata_json) VALUES($1,$2,'INCOME',1000000,'IDR',now(),'Employer','Penghasilan dari slip gaji',.99,'NEEDS_REVIEW',jsonb_build_object('period','2026-09','document_id',$3::uuid)) RETURNING id`, w.householdID, w.imageID, w.documentID).Scan(&w.proposalID))
	decision := `{"version":1,"reasonCode":"MISSING_PAY_DATE","decisionClass":"HUMAN_POLICY_CHOICE","interactionMode":"POLICY_CHOICE","knownFacts":{},"missingFacts":["transaction_at","salary_classification"],"allowedActions":["SET_PAY_DATE","PRIMARY_SALARY","ORDINARY_INCOME","IGNORE"],"decisionSource":"DETERMINISTIC"}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,proposal_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,$4,'MISSING_PAY_DATE','OPEN',$5::jsonb) RETURNING id`, w.householdID, w.proposalID, w.imageID, w.documentID, decision).Scan(&w.itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,telegram_chat_id,status) VALUES($1,$2,'MISSING_PAY_DATE',$3,'OPEN') RETURNING id`, w.itemID, w.householdID, w.chatID).Scan(&w.reqID))
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,61)`, w.reqID, w.chatID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, w.reqID)
	must(err)
	return w
}

// answerPolicy taps the salary-policy button on the card, exactly as the household
// would, so the review is left waiting only for the date.
func (w payslipReviewWorld) answerPolicy(t *testing.T, ctx context.Context) {
	t.Helper()
	raw, err := json.Marshal(callbackUpdate(w.chatID, 61, "review:salary:primary"))
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	stamp := time.Now().UnixNano()
	if err := w.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id`, w.householdID, fmt.Sprintf("cont-policy-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := NewProcessor(w.pool, payslipDateGateway{}).Process(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := w.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, w.reqID).Scan(&state); err != nil || state != "AWAITING_DATE" {
		t.Fatalf("policy answer left state %q (%v), want AWAITING_DATE", state, err)
	}
}

// reply delivers one typed message through the real Process entry and returns its
// error, so a test can also assert what happens when the reply binds to nothing.
func (w payslipReviewWorld) reply(t *testing.T, ctx context.Context, text string, replyTo int64, messageID int64) error {
	t.Helper()
	update := telegramUpdate{}
	update.Message.MessageID, update.Message.Chat.ID, update.Message.From.ID = messageID, w.chatID, w.chatID
	update.Message.Text = text
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: replyTo}
	raw, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	stamp := time.Now().UnixNano()
	if err := w.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, w.householdID, fmt.Sprintf("cont-reply-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw)); err != nil {
		t.Fatal(err)
	}
	return NewProcessor(w.pool, payslipDateGateway{}).Process(ctx, sourceID)
}

func (w payslipReviewWorld) resolved(t *testing.T, ctx context.Context) (itemStatus, payDate, amount string) {
	t.Helper()
	if err := w.pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, w.itemID).Scan(&itemStatus); err != nil {
		t.Fatal(err)
	}
	_ = w.pool.QueryRow(ctx, `SELECT (t.transaction_at AT TIME ZONE 'Asia/Jakarta')::date::text,t.amount::text
		FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id AND e.source_event_id=$1 AND e.evidence_type='PAYSLIP_IMAGE'`, w.imageID).Scan(&payDate, &amount)
	return itemStatus, payDate, amount
}

func TestDateReplyToTheUploadResolvesTheReviewWithoutReaskingKnownFacts(t *testing.T) {
	ctx := context.Background()
	w := seedPayslipReview(t, ctx)
	// "tanggal 25" answers the review whether it replies to the card or to the
	// upload; here it replies to the payslip photo itself (message 55).
	w.answerPolicy(t, ctx)
	if err := w.reply(t, ctx, "25 September 2026", 55, 70); err != nil {
		t.Fatal(err)
	}
	item, payDate, amount := w.resolved(t, ctx)
	if item != "RESOLVED" || payDate != "2026-09-25" || amount != "1000000" {
		t.Fatalf("review=%s pay date=%s amount=%s; want it resolved with the known amount untouched", item, payDate, amount)
	}
	var transactions int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, w.householdID).Scan(&transactions); err != nil || transactions != 1 {
		t.Fatalf("transactions=%d err=%v, want exactly one", transactions, err)
	}
}

func TestDateReplyToABoundNoticeResolvesTheReview(t *testing.T) {
	ctx := context.Background()
	w := seedPayslipReview(t, ctx)
	p := NewProcessor(w.pool, nil)
	if err := p.BindEvidenceMessage(ctx, w.chatID, 88, w.documentID); err != nil {
		t.Fatal(err)
	}
	w.answerPolicy(t, ctx)
	if err := w.reply(t, ctx, "25 September 2026", 88, 71); err != nil {
		t.Fatal(err)
	}
	if item, payDate, _ := w.resolved(t, ctx); item != "RESOLVED" || payDate != "2026-09-25" {
		t.Fatalf("review=%s pay date=%s after replying to the bound notice", item, payDate)
	}
}

func TestReplyToUnrelatedMessageDoesNotAnswerTheReview(t *testing.T) {
	ctx := context.Background()
	w := seedPayslipReview(t, ctx)
	// Message 999 is neither the upload, nor a bound notice, nor the card. The turn
	// falls through to the agent lane, which this fixture does not wire, so its
	// error is expected; the point is that the review stays unanswered.
	_ = w.reply(t, ctx, "25 September 2026", 999, 72)
	var item string
	if err := w.pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, w.itemID).Scan(&item); err != nil {
		t.Fatal(err)
	}
	var transactions int
	_ = w.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, w.householdID).Scan(&transactions)
	if item != "OPEN" || transactions != 0 {
		t.Fatalf("a reply to an unrelated message resolved the review: item=%s transactions=%d", item, transactions)
	}
}

func TestReplyTargetForEvidenceReviewOnlyRedirectsExactEvidenceReplies(t *testing.T) {
	ctx := context.Background()
	w := seedPayslipReview(t, ctx)
	p := NewProcessor(w.pool, nil)
	mk := func(replyTo int64, chat int64) telegramUpdate {
		u := telegramUpdate{}
		u.Message.Chat.ID, u.Message.From.ID = chat, chat
		if replyTo != 0 {
			u.Message.ReplyToMessage = &struct {
				MessageID int64 `json:"message_id"`
			}{MessageID: replyTo}
		}
		return u
	}
	if got := p.replyTargetForEvidenceReview(ctx, w.householdID, mk(55, w.chatID)); got.Message.ReplyToMessage == nil || got.Message.ReplyToMessage.MessageID != 61 {
		t.Fatalf("a reply to the upload was not redirected to the card: %+v", got.Message.ReplyToMessage)
	}
	for name, update := range map[string]telegramUpdate{
		"no reply":                        mk(0, w.chatID),
		"reply to the card itself":        mk(61, w.chatID),
		"reply to an unknown message":     mk(999, w.chatID),
		"the upload id from another chat": mk(55, w.chatID+1),
	} {
		got := p.replyTargetForEvidenceReview(ctx, w.householdID, update)
		want, have := int64(0), int64(0)
		if update.Message.ReplyToMessage != nil {
			want = update.Message.ReplyToMessage.MessageID
		}
		if got.Message.ReplyToMessage != nil {
			have = got.Message.ReplyToMessage.MessageID
		}
		if have != want {
			t.Fatalf("%s: reply target changed from %d to %d", name, want, have)
		}
	}
}
