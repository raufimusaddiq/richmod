package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// typedReplyGateway is the conversational model answering a typed reply to a
// review card: it proposes the card's value through resolve_review once.
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

// reviewAnswerEngine is Jev answering a typed review reply: it routes a plain
// message to the one open review, and for the review action either confirms or
// cannot tell (the generative model then reads the reply).
type reviewAnswerEngine struct{ reviewAction string }

func (e reviewAnswerEngine) Evaluate(ctx context.Context, model string, request judgment.Request) (judgment.Result, error) {
	result, err := eagerEngine{}.Evaluate(ctx, model, request)
	if question, ok := request.Questions["route"]; ok {
		if criteria, ok := question.Criteria.(map[string]any); ok {
			if _, has := criteria["REVIEW_INTERACTION"]; has {
				result.Answers["route"] = confidentChoice(criteria, "REVIEW_INTERACTION")
			}
		}
	}
	if question, ok := request.Questions["review_action"]; ok && e.reviewAction != "" {
		if criteria, ok := question.Criteria.(map[string]any); ok {
			if _, has := criteria[e.reviewAction]; has {
				result.Answers["review_action"] = confidentChoice(criteria, e.reviewAction)
			}
		}
	}
	return result, err
}

// typedReplyModes covers an exact reply and a plain message, each with Jev
// confirming and with Jev unable to tell.
var typedReplyModes = []struct {
	name   string
	reply  bool
	action string
}{
	{"reply, Jev confirms", true, "CONFIRM"},
	{"reply, Jev unclear", true, "OTHER_OR_UNCLEAR"},
	{"plain, Jev confirms", false, "CONFIRM"},
	{"plain, Jev unclear", false, "OTHER_OR_UNCLEAR"},
}

func sendTypedReviewReply(t *testing.T, ctx context.Context, f agentIntegrationFixture, p *Processor, reviewID, text string, reply bool) {
	t.Helper()
	mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 99))
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

// A purpose or correction typed in answer to its card is stored and, when the
// card asked for nothing else and the transaction already has its category,
// completes the review.
func TestTypedDetailReplyCompletesReview(t *testing.T) {
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
				p.SetJudgment(reviewAnswerEngine{reviewAction: mode.action})
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

// A bank email whose facts could not be verified asks the household for the
// amount and time. The model extracts them from the typed answer; Go validates
// them and queues the same completion job the Web uses.
func TestTypedBankFactsReplyQueuesCompletion(t *testing.T) {
	for _, mode := range typedReplyModes {
		if mode.action == "OTHER_OR_UNCLEAR" {
			continue // COMPLETE_BANK_FACTS always needs extracted values
		}
		t.Run(mode.name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "typed-bank")
			var accountID, listenerID, bankSourceID, itemID, reviewID string
			mustAgentTest(t, f.pool.QueryRow(ctx, "INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id", f.householdID).Scan(&accountID))
			mustAgentTest(t, f.pool.QueryRow(ctx, "INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank','notifikasi@bank.test',$2,$3) RETURNING id", f.householdID, accountID, f.userID).Scan(&listenerID))
			mustAgentTest(t, f.pool.QueryRow(ctx, "INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id", f.householdID, fmt.Sprintf("typed-bank-%d", time.Now().UnixNano()), []byte("bank")).Scan(&bankSourceID))
			_, err := f.pool.Exec(ctx, "INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'IMAP','v1','{}'::jsonb,'VALID')", bankSourceID, listenerID)
			mustAgentTest(t, err)
			decision, _ := reviewdec.Preset("UNKNOWN_BANK_TEMPLATE", "source_event", bankSourceID)
			encoded, err := json.Marshal(decision)
			mustAgentTest(t, err)
			tx, err := f.pool.Begin(ctx)
			mustAgentTest(t, err)
			mustAgentTest(t, tx.QueryRow(ctx, "INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN',$3::jsonb) RETURNING id", f.householdID, bankSourceID, string(encoded)).Scan(&itemID))
			mustAgentTest(t, ProjectReviewItem(ctx, tx, f.householdID, itemID, 0, "", f.chatID))
			mustAgentTest(t, tx.QueryRow(ctx, "SELECT id FROM review_request WHERE review_item_id=$1", itemID).Scan(&reviewID))
			mustAgentTest(t, tx.Commit(ctx))

			p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"COMPLETE_BANK_FACTS","amount_idr":"54000","transaction_at":"2026-09-23T13:45:00+07:00"}`})
			p.SetJudgment(reviewAnswerEngine{reviewAction: "COMPLETE_BANK_FACTS"})
			sendTypedReviewReply(t, ctx, f, p, reviewID, "54rb tadi siang 13.45", mode.reply)

			var payload string
			mustAgentTest(t, f.pool.QueryRow(ctx, "SELECT COALESCE(max(payload_json::text),'') FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1", itemID).Scan(&payload))
			if !strings.Contains(payload, `"amount_idr": "54000"`) || !strings.Contains(payload, "2026-09-23T13:45:00+07:00") {
				t.Fatalf("bank facts were not queued for completion: %q", payload)
			}
			var itemStatus string
			mustAgentTest(t, f.pool.QueryRow(ctx, "SELECT status FROM review_item WHERE id=$1", itemID).Scan(&itemStatus))
			if itemStatus != "OPEN" {
				t.Fatalf("the completion job owns resolution; item=%s", itemStatus)
			}
		})
	}
}

// A compound card (date and category missing) stores the typed date, then moves
// on to the category chooser rather than dropping the second fact.
func TestTypedDateOnCompoundCardAdvancesToCategory(t *testing.T) {
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
			p.SetJudgment(reviewAnswerEngine{reviewAction: mode.action})
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

// A purpose typed for an uncategorized expense is stored and the card moves to
// the category chooser; choosing a category there completes the review.
func TestTypedPurposeThenCategoryButtonCompletes(t *testing.T) {
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
	p.SetJudgment(reviewAnswerEngine{reviewAction: "CONFIRM"})
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

// A screenshot row whose amount was unreadable is a proposal-keyed review. A
// typed amount in answer to its card records the transaction.
func TestTypedAmountReplyRecordsScreenshotRow(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "typed-amount")
	var categoryID, imageSourceID, attachmentID, documentID, proposalID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, f.householdID).Scan(&categoryID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.householdID, fmt.Sprintf("typed-amount-%d", time.Now().UnixNano()), []byte("image")).Scan(&imageSourceID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,storage_ref,media_type,byte_size,content_hash,width,height) VALUES($1,$2,'image/png',10,$3,10,10) RETURNING id`, f.householdID, fmt.Sprintf("test/screenshot-%d.png", time.Now().UnixNano()), []byte(fmt.Sprint(time.Now().UnixNano()))).Scan(&attachmentID))
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
	p.SetJudgment(reviewAnswerEngine{reviewAction: "CONFIRM"})
	sendTypedReviewReply(t, ctx, f, p, reviewID, "34000", true)

	var amount, status, itemStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(t.amount::text,''),COALESCE(t.status,''),ri.status FROM review_item ri LEFT JOIN transaction_evidence te ON te.metadata_json->>'proposal_id'=ri.proposal_id::text LEFT JOIN transaction t ON t.id=te.transaction_id WHERE ri.id=$1 LIMIT 1`, itemID).Scan(&amount, &status, &itemStatus))
	if !strings.HasPrefix(amount, "34000") || itemStatus != "RESOLVED" {
		t.Fatalf("typed amount was not recorded: amount=%q transaction=%s review=%s", amount, status, itemStatus)
	}
}
