package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// assetHintGateway answers the generative turn the way the conversational agent
// would for "itu sebenarnya aku masukin ke emas": one typed resolve_review call
// with the semantic action plus the freeform wealth hint in the same turn.
type assetHintGateway struct {
	lastTools []gateway.ToolDefinition
	calls     int
	action    string
	hint      string
}

func (*assetHintGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, nil
}

func (g *assetHintGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.calls++
	if g.lastTools == nil {
		g.lastTools = request.Tools
	}
	for _, tool := range request.Tools {
		if tool.Name == "resolve_review" {
			args := map[string]any{"action": g.action, "wealth_account_hint": g.hint, "category_slug": g.hint}
			encoded, _ := json.Marshal(args)
			return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "typed-review", Name: "resolve_review", Arguments: encoded}}}, nil
		}
	}
	return gateway.AgentResponse{}, nil
}

// jevTransferAction selects the finite semantic action. Generative inference
// should receive the action preselected and extract only its freeform argument.
type jevTransferAction struct {
	tasks              []string
	answerReviewAction string
}

func (e *jevTransferAction) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	answers := map[string]judgment.Answer{}
	for key := range request.Questions {
		e.tasks = append(e.tasks, key)
	}
	if question, ok := request.Questions["route"]; ok {
		criteria, _ := question.Criteria.(map[string]any)
		answers["route"] = confidentChoice(criteria, "REVIEW_INTERACTION")
	}
	if question, ok := request.Questions["review_action"]; ok {
		criteria, _ := question.Criteria.(map[string]any)
		answer := e.answerReviewAction
		if answer == "" {
			answer = "ASSET_PURCHASE"
		}
		answers["review_action"] = confidentChoice(criteria, answer)
	}
	return judgment.Result{Model: "jev-undecided", Answers: answers}, nil
}

func createAgentTransferClassificationReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, messageID int64) (reviewID, transactionID, itemID string) {
	return createTransferClassificationReviewWithType(t, ctx, f, "EXPENSE", messageID)
}

func createTransferClassificationReviewWithType(t *testing.T, ctx context.Context, f agentIntegrationFixture, transactionType string, messageID int64) (reviewID, transactionID, itemID string) {
	t.Helper()
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,created_by_user_id) VALUES($1,$2,'NEEDS_REVIEW',4000000,'IDR',now(),'transfer review',$3) RETURNING id`, f.householdID, transactionType, f.userID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","allowedActions":["CLASSIFY_TRANSFER","OWN_ACCOUNT","HOUSEHOLD_ACCOUNT","INVESTMENT_ACCOUNT","EXPENSE","ASSET_PURCHASE","IGNORE"]}'::jsonb) RETURNING id`, f.householdID, transactionID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION','OPEN',$4) RETURNING id`, f.householdID, itemID, transactionID, f.chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_PURPOSE')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, f.chatID, messageID)
	mustAgentTest(t, err)
	return reviewID, transactionID, itemID
}

// T2/T3: an exact TRANSFER_CLASSIFICATION review over "aku masukin ke emas" must
// reach generative resolve_review with action=ASSET_PURCHASE and the freeform
// wealth hint, then resolve only against an ACTIVE canonical account in the SAME
// household. The binding kind is TRANSACTION while the semantic vocabulary is
// the transfer-classification set.
func TestT2T3TransferReviewAssetPurchaseUsesTypedWealthHint(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "core-boundary-asset")
	var goldID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role,active) VALUES($1,'Emas','Antam','ASSET','GOLD','INVESTMENT',true) RETURNING id`, f.householdID).Scan(&goldID))
	// A wrong-household gold account with the same hint must never be selected.
	var otherHousehold, otherGoldID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('other household') RETURNING id`).Scan(&otherHousehold))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role,active) VALUES($1,'Emas','Antam','ASSET','GOLD','INVESTMENT',true) RETURNING id`, otherHousehold).Scan(&otherGoldID))
	// An inactive same-household account must also be rejected.
	mustAgentTest(t, func() error {
		_, err := f.pool.Exec(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role,active) VALUES($1,'Emas Lama','Antam','ASSET','GOLD','INVESTMENT',false)`, f.householdID)
		return err
	}())

	reviewID, transactionID, itemID := createAgentTransferClassificationReview(t, ctx, f, 901)
	bound, public, count, err := NewProcessor(f.pool, nil).loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if bound == nil || bound.Kind != "TRANSACTION" || bound.ReviewType != "TRANSFER_CLASSIFICATION" || count != 1 {
		t.Fatalf("fixture review binding=%#v public=%v count=%d", bound, public, count)
	}
	f.update.Message.Text = "itu sebenarnya aku masukin ke emas"
	f.update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 901}
	payload, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, f.sourceID, payload)
	mustAgentTest(t, err)

	gatewayStub := &assetHintGateway{action: "ASSET_PURCHASE", hint: "emas"}
	processor := NewProcessor(f.pool, gatewayStub)
	judgmentStub := &jevTransferAction{}
	processor.SetJudgment(judgmentStub)
	mustAgentTest(t, processor.ProcessAgent(ctx, f.sourceID))
	if len(gatewayStub.lastTools) == 0 {
		var status string
		_ = f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, f.sourceID).Scan(&status)
		t.Fatalf("generative turn did not run; source status=%s judgment_tasks=%v gateway_calls=%d", status, judgmentStub.tasks, gatewayStub.calls)
	}

	// The exact-head schema narrowed to the transfer vocabulary and carried the
	// typed hint field.
	var sawResolveReview bool
	for _, tool := range gatewayStub.lastTools {
		if tool.Name != "resolve_review" {
			continue
		}
		sawResolveReview = true
		encoded, _ := json.Marshal(tool.Parameters)
		text := string(encoded)
		if !strings.Contains(text, "ASSET_PURCHASE") || !strings.Contains(text, "wealth_account_hint") {
			t.Fatalf("resolve_review schema did not expose the typed asset-purchase surface: %s", text)
		}
		properties := tool.Parameters["properties"].(map[string]any)
		action := properties["action"].(map[string]any)["enum"].([]string)
		if len(action) != 1 || action[0] != "ASSET_PURCHASE" {
			t.Fatalf("Jev-selected action was not preserved into the generative tool schema: %v", action)
		}
	}
	if !sawResolveReview {
		t.Fatal("generative turn was not given resolve_review")
	}

	var typ, status, purpose, related, itemStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT type,status,purpose,COALESCE(related_wealth_account_id::text,'') FROM transaction WHERE id=$1`, transactionID).Scan(&typ, &status, &purpose, &related))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&itemStatus))
	if typ != "TRANSFER" || status != "CONFIRMED" || purpose != "ASSET_PURCHASE" || related != goldID || itemStatus != "RESOLVED" {
		t.Fatalf("typ=%s status=%s purpose=%s related=%s item=%s; want the same-household active gold account %s", typ, status, purpose, related, itemStatus, goldID)
	}
	var wrongUsed int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE related_wealth_account_id=$1`, otherGoldID).Scan(&wrongUsed))
	if wrongUsed != 0 {
		t.Fatalf("wrong-household account was mutated")
	}
	_ = reviewID
}

// T4: the same exact TRANSFER_CLASSIFICATION binding resolves EXPENSE with the
// model-supplied typed category slug. Go validates the slug against the
// household's active categories; there is no phrase parser for "pengeluaran
// makan". An unknown slug is refused rather than guessed.
func TestT4TransferReviewExpenseUsesTypedCategory(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "core-boundary-expense")
	var diningID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman') RETURNING id`, f.householdID).Scan(&diningID))
	_, transactionID, itemID := createTransferClassificationReviewWithType(t, ctx, f, "UNCLASSIFIED", 902)

	bound, err := NewProcessor(f.pool, nil).exactAgentReviewBinding(ctx, f.householdID, f.chatID, 902)
	mustAgentTest(t, err)
	if bound == nil || bound.Kind != "TRANSACTION" || bound.ReviewType != "TRANSFER_CLASSIFICATION" {
		t.Fatalf("binding=%#v", bound)
	}
	state := *f.state
	state.ReviewBinding = bound
	state.ReviewBindingCount = 1
	state.Update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 902}
	p := NewProcessor(f.pool, nil)

	// An unknown slug must not mutate: Go refuses to invent a category.
	refused, _, err := p.agentResolveBoundReview(ctx, &state, gateway.ToolCall{CallID: "expense-bad", Name: "resolve_review"}, map[string]any{"action": "EXPENSE", "category_slug": "not-a-category"})
	mustAgentTest(t, err)
	if refused.Status != "MISSING_CATEGORY" {
		t.Fatalf("unknown category must be refused, got %#v", refused)
	}
	var stillNeedsReview string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&stillNeedsReview))
	if stillNeedsReview != "NEEDS_REVIEW" {
		t.Fatalf("a refused category must leave the transaction in review, got %s", stillNeedsReview)
	}

	// The typed valid slug resolves the canonical category and confirms.
	resolved, synthesize, err := p.agentResolveBoundReview(ctx, &state, gateway.ToolCall{CallID: "expense-ok", Name: "resolve_review"}, map[string]any{"action": "EXPENSE", "category_slug": "makanan-minuman"})
	mustAgentTest(t, err)
	if !synthesize || resolved.Status != "RESOLVED" {
		t.Fatalf("valid category must resolve the review: synthesize=%v result=%#v", synthesize, resolved)
	}
	var typ, status, category, itemStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT type,status,COALESCE(category_id::text,'') FROM transaction WHERE id=$1`, transactionID).Scan(&typ, &status, &category))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&itemStatus))
	if typ != "EXPENSE" || status != "CONFIRMED" || category != diningID || itemStatus != "RESOLVED" {
		t.Fatalf("typ=%s status=%s category=%s item=%s; want EXPENSE/%s", typ, status, category, itemStatus, diningID)
	}
}

// T3 negative cases: an ambiguous active Wealth hint and an inactive-only hint
// cannot mutate the bound transaction.
func TestT3TransferReviewRejectsAmbiguousAndInactiveWealthHints(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "core-boundary-wealth-reject")
	for _, name := range []string{"Emas A", "Emas B"} {
		mustAgentTest(t, func() error {
			_, err := f.pool.Exec(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role,active) VALUES($1,$2,'Antam','ASSET','GOLD','INVESTMENT',true)`, f.householdID, name)
			return err
		}())
	}
	mustAgentTest(t, func() error {
		_, err := f.pool.Exec(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role,active) VALUES($1,'Emas Lama','Antam','ASSET','GOLD','INVESTMENT',false)`, f.householdID)
		return err
	}())
	_, transactionID, itemID := createAgentTransferClassificationReview(t, ctx, f, 903)
	bound, err := NewProcessor(f.pool, nil).exactAgentReviewBinding(ctx, f.householdID, f.chatID, 903)
	mustAgentTest(t, err)
	state := *f.state
	state.ReviewBinding, state.ReviewBindingCount = bound, 1
	state.Update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 903}
	p := NewProcessor(f.pool, nil)
	for _, hint := range []string{"Emas", "Emas Lama"} {
		result, _, err := p.agentResolveBoundReview(ctx, &state, gateway.ToolCall{Name: "resolve_review"}, map[string]any{"action": "ASSET_PURCHASE", "wealth_account_hint": hint})
		mustAgentTest(t, err)
		if result.Status != "WEALTH_ACCOUNT_AMBIGUOUS" {
			t.Fatalf("hint %q must be rejected without mutation: %#v", hint, result)
		}
	}
	var status, reviewStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&status))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&reviewStatus))
	if status != "NEEDS_REVIEW" || reviewStatus != "OPEN" {
		t.Fatalf("failed Wealth resolution mutated canonical state: tx=%s review=%s", status, reviewStatus)
	}
}
