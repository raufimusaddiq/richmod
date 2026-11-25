package telegram

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTypedAssetClassificationRetainsDateResidual(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, transaction, _, request := seedAttributionReview(t, "UNCLASSIFIED", "TRANSFER_CLASSIFICATION", false)
	var account string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic asset','ASSET','GOLD','OTHER') RETURNING id`, f.householdID).Scan(&account))
	p := NewProcessor(f.pool, nil)
	mustAgentTest(t, p.BindReviewMessage(ctx, request, f.chatID, 99, ""))
	tap, err := json.Marshal(callbackUpdate(f.chatID, 99, "review:asset"))
	mustAgentTest(t, err)
	mustAgentTest(t, p.Process(ctx, f.seedSourceEvent(ctx, t, "asset-step", tap)))
	sendTypedReviewReply(t, ctx, f, p, request, "Synthetic asset", true)
	var status, kind, relationship string
	var missingDate bool
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,t.type,ri.decision->'knownFacts'->>'relationship_choice',ri.decision->'missingFacts' ? 'transaction_at' FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE t.id=$1`, transaction).Scan(&status, &kind, &relationship, &missingDate))
	if status != "NEEDS_REVIEW" || kind != "TRANSFER" || relationship != "ASSET_PURCHASE" || !missingDate {
		t.Fatalf("typed partial: status=%s type=%s relationship=%s missingDate=%v", status, kind, relationship, missingDate)
	}
	f.update.Message.Text = "2026-10-03"
	f.update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{99}
	raw, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	source := f.seedSourceEvent(ctx, t, "typed-asset-date", raw)
	_, err = f.pool.Exec(ctx, `UPDATE source_event SET source_type='TELEGRAM_TEXT' WHERE id=$1`, source)
	mustAgentTest(t, err)
	p = NewProcessor(f.pool, &typedReplyGateway{})
	mustAgentTest(t, p.ProcessAgent(ctx, source))
	var selected string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,related_wealth_account_id::text FROM transaction WHERE id=$1`, transaction).Scan(&status, &selected))
	if status != "CONFIRMED" || selected != account {
		t.Fatalf("typed completion: status=%s accountMatches=%v", status, selected == account)
	}
}
