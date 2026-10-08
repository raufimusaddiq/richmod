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
