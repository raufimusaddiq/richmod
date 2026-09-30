package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type merchantReplyGateway struct {
	calls int
}

func (*merchantReplyGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	panic("merchant reply must use the active agent path")
}

func (g *merchantReplyGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.calls++
	if g.calls == 1 {
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "merchant-reply", Name: "resolve_review", Arguments: json.RawMessage(`{"action":"CONFIRM","merchant":"New Cafe"}`)}}}, nil
	}
	return gateway.AgentResponse{Text: "Merchant disimpan. Pilih kategori."}, nil
}

func TestMerchantFirstActiveAgentReply(t *testing.T) {
	for _, mode := range []string{"remembered", "legacy-card", "unknown", "inactive-category", "unconfirmed-alias", "other-household"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "merchant-first-"+mode)
			var categoryID, merchantID, transactionID, reviewID string
			aliasHousehold := f.householdID
			if mode == "other-household" {
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('other merchant household') RETURNING id`).Scan(&aliasHousehold))
			}
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug,active) VALUES($1,'Dining','dining',$2) RETURNING id`, aliasHousehold, mode != "inactive-category").Scan(&categoryID))
			if mode != "unknown" {
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'New Cafe') RETURNING id`, aliasHousehold).Scan(&merchantID))
				_, err := f.pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'New Cafe',$2,$3,true,$4)`, aliasHousehold, merchantID, categoryID, mode != "unconfirmed-alias")
				mustAgentTest(t, err)
			}
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,'IDR',now(),$2) RETURNING id`, f.householdID, f.userID).Scan(&transactionID))
			tx, err := f.pool.Begin(ctx)
			mustAgentTest(t, err)
			mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transactionID, "UNKNOWN_MERCHANT", f.chatID, 0, "Nominal: Rp54.000"))
			mustAgentTest(t, tx.Commit(ctx))
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transactionID).Scan(&reviewID))
			var conversation string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&conversation))
			if conversation != "AWAITING_MERCHANT" {
				t.Fatalf("initial state=%s; must ask merchant first", conversation)
			}
			if mode == "legacy-card" {
				_, err := f.pool.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_CATEGORY' WHERE review_request_id=$1`, reviewID)
				mustAgentTest(t, err)
			}
			var prompt string
			var markup string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->>'text',COALESCE(payload_json->>'reply_markup','') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1 LIMIT 1`, reviewID).Scan(&prompt, &markup))
			if !strings.Contains(prompt, "nama merchant") {
				t.Fatalf("initial prompt=%q", prompt)
			}
			if !strings.Contains(markup, "review:asset") || !strings.Contains(markup, "Beli aset") || !strings.Contains(markup, "review:ignore") {
				t.Fatalf("merchant prompt must keep Beli aset and Abaikan: %q", markup)
			}
			model := &merchantReplyGateway{}
			p := NewProcessor(f.pool, model)
			p.SetJudgment(eagerEngine{})
			mustAgentTest(t, p.BindReviewMessage(ctx, reviewID, f.chatID, 99))
			f.update.Message.Text = "New Cafe"
			if mode == "remembered" || mode == "legacy-card" {
				f.update.Message.Text = "  NEW   CAFE  "
			}
			f.update.Message.ReplyToMessage = &struct {
				MessageID int64 `json:"message_id"`
			}{99}
			raw, err := json.Marshal(f.update)
			mustAgentTest(t, err)
			_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, f.sourceID, string(raw))
			mustAgentTest(t, err)
			mustAgentTest(t, p.ProcessAgent(ctx, f.sourceID))
			var status, gotCategory, name string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.category_id::text,''),m.normalized_name FROM transaction t JOIN merchant m ON m.id=t.merchant_id WHERE t.id=$1`, transactionID).Scan(&status, &gotCategory, &name))
			var stored []byte
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT decision FROM review_item WHERE transaction_id=$1`, transactionID).Scan(&stored))
			if blocked := residualConfirmationBlockers(stored, false, true, false); len(blocked) != 0 {
				t.Fatalf("merchant reply did not advance decision: %s", stored)
			}
			if mode == "remembered" || mode == "legacy-card" {
				if status != "CONFIRMED" || gotCategory != categoryID || model.calls != 0 {
					t.Fatalf("learned merchant result=%s category=%s calls=%d", status, gotCategory, model.calls)
				}
				mustAgentTest(t, p.ProcessAgent(ctx, f.sourceID)) // idempotent replay
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&conversation))
				if conversation != "RESOLVED" {
					t.Fatalf("already learned alias must not ask to learn again: %s", conversation)
				}
			} else {
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&conversation))
				if status != "NEEDS_REVIEW" || gotCategory != "" || conversation != "AWAITING_CATEGORY" {
					t.Fatalf("unsafe alias applied: %s category=%s state=%s", status, gotCategory, conversation)
				}
			}
		})
	}
}
