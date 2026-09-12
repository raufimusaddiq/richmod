package telegram

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type agentTransferReviewFixture struct {
	caseID, reviewID, itemID, sourceID string
}

func createAgentTransferReviewForChat(t *testing.T, ctx context.Context, f agentIntegrationFixture, accountID string, chatID, messageID int64) agentTransferReviewFixture {
	t.Helper()
	var sourceID, caseID, itemID, reviewID string
	external := fmt.Sprintf("transfer-review-%d-%d-%d", chatID, messageID, time.Now().UnixNano())
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.householdID, external, []byte(external)).Scan(&sourceID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,candidate_transaction_ids,status) VALUES($1,$2,$3,125000,now(),'transfer review','INTERNAL_TRANSFER','{}','OPEN') RETURNING id`, f.householdID, sourceID, accountID).Scan(&caseID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN') RETURNING id`, f.householdID, sourceID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,review_type,status,telegram_chat_id) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN',$3) RETURNING id`, f.householdID, itemID, chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, chatID, messageID)
	mustAgentTest(t, err)
	return agentTransferReviewFixture{caseID: caseID, reviewID: reviewID, itemID: itemID, sourceID: sourceID}
}

type agentWealthReviewFixture struct {
	observationID, reviewID, itemID string
}

func createAgentWealthReviewForChat(t *testing.T, ctx context.Context, f agentIntegrationFixture, chatID, messageID int64) agentWealthReviewFixture {
	t.Helper()
	var observationID, itemID, reviewID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_observation(household_id,institution,account_hint,observed_value_idr,status) VALUES($1,'Test Bank','Investasi',5000000,'PENDING') RETURNING id`, f.householdID).Scan(&observationID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,wealth_observation_id,review_type,status) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN') RETURNING id`, f.householdID, observationID).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,review_type,status,telegram_chat_id) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN',$3) RETURNING id`, f.householdID, itemID, chatID).Scan(&reviewID))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, reviewID, chatID, messageID)
	mustAgentTest(t, err)
	return agentWealthReviewFixture{observationID: observationID, reviewID: reviewID, itemID: itemID}
}

func TestAgentTransferReviewBindingIgnoresAnotherHouseholdChat(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "transfer-chat-scope")
	var accountID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Scope Account','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&accountID))
	wanted := createAgentTransferReviewForChat(t, ctx, f, accountID, f.chatID, 101)
	createAgentTransferReviewForChat(t, ctx, f, accountID, f.chatID+999, 202)

	p := NewProcessor(f.pool, nil)
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 || binding.Kind != "TRANSFER_RECONCILIATION" || binding.TargetID != wanted.caseID {
		t.Fatalf("binding=%#v count=%d; want transfer case %s only", binding, count, wanted.caseID)
	}
}

func TestAgentWealthReviewBindingIgnoresAnotherHouseholdChat(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "wealth-chat-scope")
	wanted := createAgentWealthReviewForChat(t, ctx, f, f.chatID, 301)
	createAgentWealthReviewForChat(t, ctx, f, f.chatID+999, 302)

	p := NewProcessor(f.pool, nil)
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 || binding.Kind != "WEALTH_OBSERVATION" || binding.TargetID != wanted.observationID {
		t.Fatalf("binding=%#v count=%d; want wealth observation %s only", binding, count, wanted.observationID)
	}
}

func TestAgentBoundTransferExactReplyCannotDriftToNewerCase(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "transfer-exact-drift")
	var accountID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Drift Account','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&accountID))
	first := createAgentTransferReviewForChat(t, ctx, f, accountID, f.chatID, 401)
	second := createAgentTransferReviewForChat(t, ctx, f, accountID, f.chatID, 402)

	update := f.update
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 401}
	p := NewProcessor(f.pool, nil)
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 || binding.TargetID != first.caseID {
		t.Fatalf("exact binding=%#v count=%d; want first case", binding, count)
	}
	state := *f.state
	state.Update = update
	state.ReviewBinding = binding
	state.ReviewBindingCount = 1
	result, synthesize, err := p.agentResolveBoundReview(ctx, &state, gateway.ToolCall{CallID: "ignore-bound-transfer", Name: "resolve_review"}, map[string]any{"action": "IGNORE"})
	mustAgentTest(t, err)
	if !synthesize || result.Status != "RESOLVED" || result.Mutation["action"] != "TRANSFER_RECONCILIATION_IGNORED" {
		t.Fatalf("result=%#v synthesize=%v", result, synthesize)
	}
	var firstStatus, secondStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transfer_reconciliation_case WHERE id=$1`, first.caseID).Scan(&firstStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transfer_reconciliation_case WHERE id=$1`, second.caseID).Scan(&secondStatus))
	if firstStatus != "DISMISSED" || secondStatus != "OPEN" {
		t.Fatalf("target drift: first=%s second=%s", firstStatus, secondStatus)
	}
}

func TestAgentTransferIgnoreUsesDismissLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "transfer-ignore")
	var accountID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Ignore Account','BANK','FULL_LEDGER') RETURNING id`, f.householdID).Scan(&accountID))
	created := createAgentTransferReviewForChat(t, ctx, f, accountID, f.chatID, 501)
	p := NewProcessor(f.pool, nil)
	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 {
		t.Fatalf("binding=%#v count=%d", binding, count)
	}
	f.state.ReviewBinding = binding
	f.state.ReviewBindingCount = 1
	result, _, err := p.agentResolveBoundReview(ctx, f.state, gateway.ToolCall{CallID: "ignore-transfer", Name: "resolve_review"}, map[string]any{"action": "IGNORE"})
	mustAgentTest(t, err)
	if result.Mutation["action"] != "TRANSFER_RECONCILIATION_IGNORED" {
		t.Fatalf("mutation=%#v", result.Mutation)
	}
	var caseStatus, itemStatus, resolution, requestStatus, originalStatus, commandStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transfer_reconciliation_case WHERE id=$1`, created.caseID).Scan(&caseStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, created.itemID).Scan(&itemStatus, &resolution))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM review_request WHERE id=$1`, created.reviewID).Scan(&requestStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, created.sourceID).Scan(&originalStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, f.sourceID).Scan(&commandStatus))
	if caseStatus != "DISMISSED" || itemStatus != "RESOLVED" || resolution != "IGNORED" || requestStatus != "RESOLVED" || originalStatus != "IGNORED" || commandStatus != "PROCESSED" {
		t.Fatalf("ignore lifecycle case=%s item=%s/%s request=%s original=%s command=%s", caseStatus, itemStatus, resolution, requestStatus, originalStatus, commandStatus)
	}
	var reconciled int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE id=$1 AND resolution_action='TRANSFER_RECONCILED'`, created.itemID).Scan(&reconciled))
	if reconciled != 0 {
		t.Fatalf("ignored transfer was incorrectly marked reconciled")
	}
}

func TestAgentBoundWealthReviewRejectsWrongChatAtMutationTime(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "wealth-bound-chat")
	otherChat := f.chatID + 999
	created := createAgentWealthReviewForChat(t, ctx, f, otherChat, 601)
	p := NewProcessor(f.pool, nil)
	binding, err := p.exactAgentReviewBinding(ctx, f.householdID, otherChat, 601)
	mustAgentTest(t, err)
	if binding == nil || binding.TargetID != created.observationID {
		t.Fatalf("other-chat binding=%#v", binding)
	}
	state := *f.state
	state.ReviewBinding = binding
	state.ReviewBindingCount = 1
	result, _, err := p.agentResolveBoundReview(ctx, &state, gateway.ToolCall{CallID: "wrong-chat-wealth", Name: "resolve_review"}, map[string]any{"action": "IGNORE"})
	mustAgentTest(t, err)
	if result.Status != "STALE_REVIEW_BINDING" {
		t.Fatalf("wrong-chat result=%#v", result)
	}
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM wealth_observation WHERE id=$1`, created.observationID).Scan(&status))
	if status != "PENDING" {
		t.Fatalf("wrong-chat wealth observation mutated to %s", status)
	}
}
