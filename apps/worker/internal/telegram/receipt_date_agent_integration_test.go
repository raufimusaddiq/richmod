package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// receiptDateGateway is the conversational model answering a reply to a receipt
// date card: it proposes the date through resolve_review, then writes prose.
type receiptDateGateway struct {
	arguments string
	calls     int
}

func (*receiptDateGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	panic("a typed reply must use the conversational agent")
}

func (g *receiptDateGateway) AgentTurn(context.Context, string, gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.calls++
	if g.calls == 1 {
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "date-reply", Name: "resolve_review", Arguments: json.RawMessage(g.arguments)}}}, nil
	}
	return gateway.AgentResponse{Text: "Tanggal disimpan."}, nil
}

// A receipt whose date is missing is a transaction-keyed review owned by the
// conversational agent. The model proposes the date the household typed; Go
// validates it, stores it as the transaction date and completes the review.
func TestAgentReplyStoresReceiptTransactionDate(t *testing.T) {
	// Production always runs with the judgment plane (Jev); without it the agent
	// has no side-effect tools at all.
	cases := map[string]string{
		"transaction_at":           `{"action":"CONFIRM","transaction_at":"2026-10-03"}`,
		"transaction_at, unpadded": `{"action":"CONFIRM","transaction_at":"2026-10-3"}`,
		"pay_date":                 `{"action":"CONFIRM","pay_date":"2026-10-03"}`,
		"RFC3339 timestamp":        `{"action":"CONFIRM","transaction_at":"2026-10-03T22:55:00+07:00"}`,
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "receipt-date")
			var categoryID, transactionID, reviewID string
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, f.householdID).Scan(&categoryID))
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34000,'IDR','2026-09-01T12:00:00+07:00',$2,'Indomaret',$3) RETURNING id`, f.householdID, categoryID, f.userID).Scan(&transactionID))
			decision, ok := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", transactionID)
			if !ok {
				t.Fatal("no preset for MISSING_TRANSACTION_DATE")
			}
			tx, err := f.pool.Begin(ctx)
			mustAgentTest(t, err)
			mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transactionID, "MISSING_TRANSACTION_DATE", f.chatID, 0, "", decision))
			mustAgentTest(t, tx.Commit(ctx))
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&reviewID))
			var state string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&state))
			if state != "AWAITING_DATE" {
				t.Fatalf("receipt date card state=%s, want AWAITING_DATE", state)
			}

			model := &receiptDateGateway{arguments: arguments}
			p := NewProcessor(f.pool, model)
			p.SetJudgment(eagerEngine{})
			mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 99, ""))
			f.update.Message.Text = "2026-10-3"
			f.update.Message.ReplyToMessage = &struct {
				MessageID int64 `json:"message_id"`
			}{99}
			raw, err := json.Marshal(f.update)
			mustAgentTest(t, err)
			_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, f.sourceID, string(raw))
			mustAgentTest(t, err)

			mustAgentTest(t, p.ProcessAgent(ctx, f.sourceID))

			var at time.Time
			var status, description, itemStatus string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.transaction_at,t.status,COALESCE(t.description,''),ri.status FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE t.id=$1`, transactionID).Scan(&at, &status, &description, &itemStatus))
			if got := at.In(jakartaLocation()).Format("2006-01-02"); got != "2026-10-03" {
				t.Fatalf("transaction_at=%s, want 2026-10-03", got)
			}
			if description != "Indomaret" {
				t.Fatalf("the date reply overwrote the description: %q", description)
			}
			if status != "CONFIRMED" || itemStatus != "RESOLVED" {
				t.Fatalf("transaction=%s review=%s, want CONFIRMED/RESOLVED", status, itemStatus)
			}
		})
	}
}

// A proposed value that is not a calendar date stores nothing: the date, the
// description and the open review are untouched, and the review still asks for
// the date.
func TestAgentReplyRejectsNonDateForReceiptDateReview(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "receipt-date-invalid")
	var categoryID, transactionID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34000,'IDR','2026-09-01T12:00:00+07:00',$2,'Indomaret',$3) RETURNING id`, f.householdID, categoryID, f.userID).Scan(&transactionID))
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", transactionID)
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transactionID, "MISSING_TRANSACTION_DATE", f.chatID, 0, "", decision))
	mustAgentTest(t, tx.Commit(ctx))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&reviewID))

	p := NewProcessor(f.pool, &receiptDateGateway{arguments: `{"action":"CONFIRM","transaction_at":"besok lusa"}`})
	p.SetJudgment(eagerEngine{})
	mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 99, ""))
	f.update.Message.Text = "besok lusa"
	f.update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{99}
	raw, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, f.sourceID, string(raw))
	mustAgentTest(t, err)

	mustAgentTest(t, p.ProcessAgent(ctx, f.sourceID))

	var at time.Time
	var status, description, state string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.transaction_at,t.status,COALESCE(t.description,''),c.state FROM transaction t JOIN review_request r ON r.transaction_id=t.id JOIN review_conversation c ON c.review_request_id=r.id WHERE t.id=$1`, transactionID).Scan(&at, &status, &description, &state))
	if got := at.In(jakartaLocation()).Format("2006-01-02"); got != "2026-09-01" || status != "NEEDS_REVIEW" || description != "Indomaret" || state != "AWAITING_DATE" {
		t.Fatalf("a non-date changed the review: date=%s status=%s description=%q state=%s", got, status, description, state)
	}
}

// A receipt card says which transaction it is about even though the document
// producer sends no summary, and its date prompt appears once.
func TestReceiptReviewCardNamesItsTransaction(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "receipt-card-summary")
	var merchantID, transactionID, itemID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Indomaret') RETURNING id`, f.householdID).Scan(&merchantID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34000,'IDR',now(),$2,$3) RETURNING id`, f.householdID, merchantID, f.userID).Scan(&transactionID))
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", transactionID)
	raw, err := json.Marshal(decision)
	mustAgentTest(t, err)
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'MISSING_TRANSACTION_DATE','OPEN',$3::jsonb) RETURNING id`, f.householdID, transactionID, string(raw)).Scan(&itemID))
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	mustAgentTest(t, ProjectReviewItem(ctx, tx, f.householdID, itemID, 0, "", f.chatID))
	mustAgentTest(t, tx.Commit(ctx))

	var text string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE review_item_id=$1)`, itemID).Scan(&text))
	for _, want := range []string{"Nominal: Rp34.000", "Merchant: Indomaret", "YYYY-MM-DD"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card %q is missing %q", text, want)
		}
	}
	if got := strings.Count(text, "Tanggal transaksi belum ada"); got != 1 {
		t.Fatalf("title appears %d times in %q", got, text)
	}
}

// A refused date review is worded as a refusal even when the model call that
// would have phrased it fails, so the household never reads it as saved.
func TestRefusedReviewDetailIsNotReportedAsDone(t *testing.T) {
	generic := agentMutationFallback(agentToolResult{Status: "SOME_UNKNOWN_STATUS"})
	text := agentMutationFallback(agentToolResult{Status: "INVALID_TRANSACTION_DATE"})
	if text == generic || !strings.Contains(text, "belum ada yang disimpan") {
		t.Fatalf("INVALID_TRANSACTION_DATE fallback reads as success: %q", text)
	}
}
