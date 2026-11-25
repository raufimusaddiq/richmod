package telegram

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type agentWealthReviewFixture struct {
	observationID, reviewID, itemID string
}

func createAgentWealthReviewForChat(t *testing.T, ctx context.Context, f agentIntegrationFixture, chatID, messageID int64) agentWealthReviewFixture {
	t.Helper()
	var observationID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_observation(household_id,institution,account_hint,observed_value_idr,status) VALUES($1,'Test Bank','Investasi',5000000,'PENDING') RETURNING id`, f.householdID).Scan(&observationID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,wealth_observation_id,review_type,status,decision) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN','{"version":1,"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","allowedActions":["SET_WEALTH_ACCOUNT","IGNORE"]}'::jsonb) RETURNING id`, f.householdID, observationID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,review_type,status,telegram_chat_id) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN',$3) RETURNING id`, f.householdID, itemID, chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, chatID, messageID)
	mustAgentTest(t, err)
	return agentWealthReviewFixture{observationID: observationID, reviewID: reviewID, itemID: itemID}
}

type assetPurchaseScene struct {
	f         agentIntegrationFixture
	p         *Processor
	created   agentWealthReviewFixture
	binding   *agentReviewBinding
	accountID string
	wealthID  string
}

func newAssetPurchaseScene(t *testing.T, label string, messageID int64) assetPurchaseScene {
	t.Helper()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, label)
	scene := assetPurchaseScene{f: f, p: NewProcessor(f.pool, nil)}
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&scene.accountID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Bibit','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, f.householdID).Scan(&scene.wealthID))
	scene.created = createAgentWealthReviewForChat(t, ctx, f, f.chatID, messageID)
	binding, err := scene.p.exactAgentReviewBinding(ctx, f.householdID, f.chatID, messageID)
	mustAgentTest(t, err)
	if binding == nil || binding.TargetID != scene.created.observationID {
		t.Fatalf("binding=%#v", binding)
	}
	scene.binding = binding
	return scene
}

func (s assetPurchaseScene) state() *agentState {
	state := *s.f.state
	state.ReviewBinding = s.binding
	state.Now = time.Now().In(jakartaLocation())
	state.Update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: s.binding.TelegramMessageID}
	return &state
}

func (s assetPurchaseScene) propose(t *testing.T, state *agentState) agentToolResult {
	t.Helper()
	args := map[string]any{"source_account_hint": "Jago", "wealth_account_hint": "Bibit", "date_reference": "TODAY", "explicit_date": nil, "local_time": "09:30"}
	result, _, err := s.p.agentProposeAssetPurchase(context.Background(), state, gateway.ToolCall{CallID: "asset-purchase", Name: toolProposeAssetPurchase}, args)
	mustAgentTest(t, err)
	return result
}

func (s assetPurchaseScene) confirm(t *testing.T, yes bool, token string) bool {
	t.Helper()
	handled, err := s.p.processPendingAssetPurchase(context.Background(), s.f.householdID, s.f.update, s.f.sourceID, yes, token)
	mustAgentTest(t, err)
	return handled
}

func (s assetPurchaseScene) wantUntouched(t *testing.T) {
	t.Helper()
	var transactions int
	var observation, review string
	mustAgentTest(t, s.f.pool.QueryRow(context.Background(), `SELECT count(*) FROM transaction WHERE household_id=$1 AND purpose='ASSET_PURCHASE'`, s.f.householdID).Scan(&transactions))
	mustAgentTest(t, s.f.pool.QueryRow(context.Background(), `SELECT wo.status,r.status FROM wealth_observation wo,review_request r WHERE wo.id=$1 AND r.id=$2`, s.created.observationID, s.created.reviewID).Scan(&observation, &review))
	if transactions != 0 || observation != "PENDING" || review != "OPEN" {
		t.Fatalf("asset purchase leaked: transactions=%d observation=%s review=%s", transactions, observation, review)
	}
}

func TestAssetPurchaseRecordsOnlyAfterTheUserConfirms(t *testing.T) {
	t.Parallel()
	scene := newAssetPurchaseScene(t, "asset-purchase-confirm", 701)
	result := scene.propose(t, scene.state())
	if result.Status != "PENDING_CONFIRMATION" || result.confirmationToken == "" {
		t.Fatalf("proposal result = %#v", result)
	}
	scene.wantUntouched(t)
	if markup := agentPendingMarkup([]agentToolResult{result}); markup == nil || len(markup.InlineKeyboard[0]) != 2 {
		t.Fatalf("proposal has no Ya/Batal buttons: %#v", markup)
	}
	if !scene.confirm(t, true, result.confirmationToken) {
		t.Fatal("confirm tap was not handled")
	}
	ctx := context.Background()
	var transactions int
	var observation, review, source string
	mustAgentTest(t, scene.f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND account_id=$2 AND type='TRANSFER' AND status='CONFIRMED' AND purpose='ASSET_PURCHASE' AND related_wealth_account_id=$3 AND amount=5000000`, scene.f.householdID, scene.accountID, scene.wealthID).Scan(&transactions))
	mustAgentTest(t, scene.f.pool.QueryRow(ctx, `SELECT wo.status,r.status,s.processing_status FROM wealth_observation wo,review_request r,source_event s WHERE wo.id=$1 AND r.id=$2 AND s.id=$3`, scene.created.observationID, scene.created.reviewID, scene.f.sourceID).Scan(&observation, &review, &source))
	if transactions != 1 || observation != "DISMISSED" || review != "RESOLVED" || source != "PROCESSED" {
		t.Fatalf("inconsistent outcome transactions=%d observation=%s review=%s source=%s", transactions, observation, review, source)
	}
	if scene.confirm(t, true, result.confirmationToken) {
		t.Fatal("a used token confirmed a second time")
	}
}

func TestAssetPurchaseCancelAndWrongBindingRecordNothing(t *testing.T) {
	t.Parallel()
	scene := newAssetPurchaseScene(t, "asset-purchase-cancel", 702)
	result := scene.propose(t, scene.state())
	if !scene.confirm(t, false, result.confirmationToken) {
		t.Fatal("cancel tap was not handled")
	}
	scene.wantUntouched(t)
	if scene.confirm(t, true, result.confirmationToken) {
		t.Fatal("a cancelled proposal confirmed")
	}
	scene.wantUntouched(t)

	stale := newAssetPurchaseScene(t, "asset-purchase-inactive", 705)
	staged := stale.propose(t, stale.state())
	_, err := stale.f.pool.Exec(context.Background(), `UPDATE account SET active=false WHERE id=$1`, stale.accountID)
	mustAgentTest(t, err)
	if !stale.confirm(t, true, staged.confirmationToken) {
		t.Fatal("confirm tap was not handled")
	}
	stale.wantUntouched(t)

	otherChat := newAssetPurchaseScene(t, "asset-purchase-chat", 703)
	state := otherChat.state()
	state.Update.Message.Chat.ID = otherChat.f.chatID + 999
	if got := otherChat.propose(t, state); got.Status != "STALE_REVIEW_BINDING" {
		t.Fatalf("proposal from another chat = %#v", got)
	}
	otherChat.wantUntouched(t)
}

func TestAssetPurchaseConfirmationRollsBackWhenALaterWriteFails(t *testing.T) {
	t.Parallel()
	scene := newAssetPurchaseScene(t, "asset-purchase-rollback", 704)
	result := scene.propose(t, scene.state())
	scene.f.sourceID = "00000000-0000-0000-0000-000000000000"
	handled, err := scene.p.processPendingAssetPurchase(context.Background(), scene.f.householdID, scene.f.update, scene.f.sourceID, true, result.confirmationToken)
	if err == nil || !handled {
		t.Fatalf("expected the evidence write to fail, handled=%v err=%v", handled, err)
	}
	scene.wantUntouched(t)
}

func TestAssetPurchaseToolIsOfferedOnlyForABoundBalanceScreenshot(t *testing.T) {
	t.Parallel()
	if assetPurchaseOffered(nil) || assetPurchaseOffered(&agentReviewBinding{Kind: "TRANSACTION", TargetID: "x", ReviewRequestID: "y"}) {
		t.Fatal("asset purchase offered without a wealth observation binding")
	}
	if !assetPurchaseOffered(&agentReviewBinding{Kind: wealthObservationBindingKind, TargetID: "x", ReviewRequestID: "y"}) {
		t.Fatal("asset purchase not offered for a wealth observation binding")
	}
	call := gateway.ToolCall{Name: toolProposeAssetPurchase, Arguments: json.RawMessage(`{"source_account_hint":"Jago","wealth_account_hint":null,"date_reference":"EXPLICIT","explicit_date":null,"local_time":null}`)}
	if _, err := validateProposeAssetPurchase(call); err == nil {
		t.Fatal("an explicit date without a date was accepted")
	}
}
