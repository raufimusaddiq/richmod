package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type typedReplyGateway struct {
	arguments string
	calls     int
}

func (*typedReplyGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("a typed review reply must use the conversational agent")
}

func (g *typedReplyGateway) AgentTurn(context.Context, string, gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.calls++
	if g.calls == 1 {
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "typed-reply", Name: "resolve_review", Arguments: json.RawMessage(g.arguments)}}}, nil
	}
	return gateway.AgentResponse{Text: "Sudah dicatat."}, nil
}

var typedReplyModes = []struct {
	name  string
	reply bool
}{
	{"reply", true},
	{"plain", false},
}

func sendTypedReviewReply(t *testing.T, ctx context.Context, f agentIntegrationFixture, p *Processor, reviewID, text string, reply bool) {
	t.Helper()
	mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 99, ""))
	f.update.Message.Text = text
	if reply {
		f.update.Message.ReplyToMessage = &struct {
			MessageID int64 `json:"message_id"`
		}{99}
	}
	raw, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, f.sourceID, string(raw))
	mustAgentTest(t, err)
	mustAgentTest(t, p.ProcessAgent(ctx, f.sourceID))
}

func TestTypedDetailReplyCompletesReview(t *testing.T) {
	t.Parallel()
	for _, reviewType := range []string{"UNKNOWN_PURPOSE", "MANUAL_CORRECTION"} {
		for _, mode := range typedReplyModes {
			t.Run(reviewType+"/"+mode.name, func(t *testing.T) {
				ctx := context.Background()
				f := newAgentIntegrationFixture(t, "typed-detail")
				var categoryID, transactionID, reviewID string
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34000,'IDR','2026-09-01T12:00:00+07:00',$2,'before',$3) RETURNING id`, f.householdID, categoryID, f.userID).Scan(&transactionID))
				decision, ok := reviewdec.Preset(reviewType, "transaction", transactionID)
				if !ok {
					t.Fatalf("no preset for %s", reviewType)
				}
				tx, err := f.pool.Begin(ctx)
				mustAgentTest(t, err)
				mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transactionID, reviewType, f.chatID, 0, "Nominal: Rp34.000", decision))
				mustAgentTest(t, tx.Commit(ctx))
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&reviewID))

				p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","description":"makan siang tim"}`})
				sendTypedReviewReply(t, ctx, f, p, reviewID, "makan siang tim", mode.reply)

				var status, description, itemStatus string
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.description,''),ri.status FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE t.id=$1`, transactionID).Scan(&status, &description, &itemStatus))
				if description != "makan siang tim" {
					t.Fatalf("the typed detail was not stored: %q", description)
				}
				if status != "CONFIRMED" || itemStatus != "RESOLVED" {
					t.Fatalf("transaction=%s review=%s, want CONFIRMED/RESOLVED (category already known)", status, itemStatus)
				}
			})
		}
	}
}

func TestTypedDateOnCompoundCardAdvancesToCategory(t *testing.T) {
	t.Parallel()
	for _, mode := range typedReplyModes {
		t.Run(mode.name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "typed-compound")
			var transactionID, reviewID string
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(new(string)))
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34000,'IDR','2026-09-01T12:00:00+07:00',$2) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
			decision, _ := reviewdec.Preset("TRANSACTION_FACTS_MISSING", "transaction", transactionID)
			tx, err := f.pool.Begin(ctx)
			mustAgentTest(t, err)
			mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transactionID, "TRANSACTION_FACTS_MISSING", f.chatID, 0, "Nominal: Rp34.000", decision))
			mustAgentTest(t, tx.Commit(ctx))
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&reviewID))

			p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","transaction_at":"2026-10-03"}`})
			sendTypedReviewReply(t, ctx, f, p, reviewID, "2026-10-03", mode.reply)

			var at time.Time
			var status, state string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.transaction_at,t.status,c.state FROM transaction t JOIN review_request r ON r.transaction_id=t.id JOIN review_conversation c ON c.review_request_id=r.id WHERE t.id=$1`, transactionID).Scan(&at, &status, &state))
			if got := at.In(jakartaLocation()).Format("2006-01-02"); got != "2026-10-03" || status != "NEEDS_REVIEW" || state != "AWAITING_CATEGORY" {
				t.Fatalf("date=%s transaction=%s state=%s, want 2026-10-03/NEEDS_REVIEW/AWAITING_CATEGORY", got, status, state)
			}
		})
	}
}

func TestTypedPurposeThenCategoryButtonCompletes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "typed-purpose-chooser")
	var categoryID, transactionID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',34000,'IDR','2026-09-01T12:00:00+07:00','before',$2) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
	decision, _ := reviewdec.Preset("UNKNOWN_PURPOSE", "transaction", transactionID)
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transactionID, "UNKNOWN_PURPOSE", f.chatID, 0, "Nominal: Rp34.000", decision))
	mustAgentTest(t, tx.Commit(ctx))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&reviewID))

	p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","description":"makan siang tim"}`})
	sendTypedReviewReply(t, ctx, f, p, reviewID, "makan siang tim", true)

	var description, state string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(t.description,''),c.state FROM transaction t JOIN review_request r ON r.transaction_id=t.id JOIN review_conversation c ON c.review_request_id=r.id WHERE t.id=$1`, transactionID).Scan(&description, &state))
	if description != "makan siang tim" || state != "AWAITING_CATEGORY" {
		t.Fatalf("description=%q state=%s, want the purpose stored and the chooser next", description, state)
	}

	raw, err := json.Marshal(callbackUpdate(f.chatID, 99, "review:cat:"+categoryID))
	mustAgentTest(t, err)
	mustAgentTest(t, p.Process(ctx, f.seedSourceEvent(ctx, t, "category-button", raw)))
	var status, gotCategory string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,COALESCE(category_id::text,'') FROM transaction WHERE id=$1`, transactionID).Scan(&status, &gotCategory))
	if status != "CONFIRMED" || gotCategory != categoryID {
		t.Fatalf("category button did not complete the review: status=%s category=%s", status, gotCategory)
	}
}

func TestTypedAmountReplyRecordsScreenshotRow(t *testing.T) {
	t.Parallel()
	runTypedAmountReply(t, "typed-amount", "34000", "34000")
}

func TestTypedAmountReplyAcceptsShorthand(t *testing.T) {
	t.Parallel()
	runTypedAmountReply(t, "typed-amount-shorthand", "50rb", "50000")
}

func runTypedAmountReply(t *testing.T, label, reply, wantAmount string) {
	t.Helper()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, label)
	var categoryID, imageSourceID, attachmentID, documentID, proposalID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.householdID, fmt.Sprintf("typed-amount-%d", testsupport.UniqueStamp()), []byte("image")).Scan(&imageSourceID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,storage_ref,media_type,byte_size,content_hash,width,height) VALUES($1,$2,'image/png',10,$3,10,10) RETURNING id`, f.householdID, fmt.Sprintf("test/screenshot-%d.png", testsupport.UniqueStamp()), []byte(fmt.Sprint(testsupport.UniqueStamp()))).Scan(&attachmentID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status,document_type) VALUES($1,$2,$3,'NEEDS_REVIEW','EWALLET_SCREENSHOT') RETURNING id`, f.householdID, imageSourceID, attachmentID).Scan(&documentID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,confidence,proposal_status,metadata_json)
		VALUES($1,$2,'row-001','EXPENSE',NULL,'IDR','2026-10-03T12:00:00+07:00','Indomaret',$3,0.9,'NEEDS_REVIEW',jsonb_build_object('date_known',true,'row_index',0,'document_id',$4::text)) RETURNING id`, f.householdID, imageSourceID, categoryID, documentID).Scan(&proposalID))
	decision := `{"version":1,"reasonCode":"MISSING_AMOUNT","decisionClass":"EVIDENCE_GAP","knownFacts":{},"missingFacts":["amount"],"allowedActions":["CONFIRM_REVIEW","IGNORE"],"interactionMode":"SINGLE_FIELD"}`
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	mustAgentTest(t, tx.QueryRow(ctx, `INSERT INTO review_item(household_id,proposal_id,review_type,status,decision) VALUES($1,$2,'MISSING_AMOUNT','OPEN',$3::jsonb) RETURNING id`, f.householdID, proposalID, decision).Scan(&itemID))
	mustAgentTest(t, ProjectReviewItem(ctx, tx, f.householdID, itemID, 0, "", f.chatID))
	mustAgentTest(t, tx.QueryRow(ctx, `SELECT id FROM review_request WHERE review_item_id=$1`, itemID).Scan(&reviewID))
	mustAgentTest(t, tx.Commit(ctx))

	p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM"}`})
	sendTypedReviewReply(t, ctx, f, p, reviewID, reply, true)

	var amount, status, itemStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(t.amount::text,''),COALESCE(t.status,''),ri.status FROM review_item ri LEFT JOIN transaction_evidence te ON te.metadata_json->>'proposal_id'=ri.proposal_id::text LEFT JOIN transaction t ON t.id=te.transaction_id WHERE ri.id=$1 LIMIT 1`, itemID).Scan(&amount, &status, &itemStatus))
	if !strings.HasPrefix(amount, wantAmount) || itemStatus != "RESOLVED" {
		t.Fatalf("typed amount was not recorded: amount=%q transaction=%s review=%s", amount, status, itemStatus)
	}
}
