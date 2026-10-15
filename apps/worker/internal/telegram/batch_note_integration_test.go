package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAuditF14BatchRetainsNote(t *testing.T) {
	for _, lane := range []string{"callback", "agent"} {
		t.Run(lane, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "batch-note")
			p := NewProcessor(f.pool, nil)
			staged, _, err := p.agentStageBatch(ctx, f.state, gateway.ToolCall{}, map[string]any{"items": []any{map[string]any{
				"type": "INCOME", "amount_idr": "100", "description": "synthetic", "note": "synthetic note",
				"date_reference": "TODAY", "date_provenance": "USER_STATED", "confidence": 1.0,
			}}})
			mustAgentTest(t, err)
			var stagedNote string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(items_json->0->>'Note','') FROM telegram_pending_batch WHERE household_id=$1`, f.householdID).Scan(&stagedNote))
			if lane == "callback" {
				_, err = p.processPendingBatch(ctx, f.householdID, f.update, f.sourceID, true, staged.confirmationToken)
			} else {
				_, _, err = p.agentFinalizePendingBatch(ctx, f.state, gateway.ToolCall{}, true, nil)
			}
			mustAgentTest(t, err)
			var note, proposalNote string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(t.note,''),COALESCE(p.note,'') FROM transaction t JOIN transaction_evidence te ON te.transaction_id=t.id JOIN transaction_proposal p ON p.id=(te.metadata_json->>'proposal_id')::uuid WHERE t.household_id=$1`, f.householdID).Scan(&note, &proposalNote))
			public := staged.Mutation["items"].([]map[string]any)
			if stagedNote != "synthetic note" || note != stagedNote || proposalNote != stagedNote || public[0]["note"] != stagedNote {
				t.Fatalf("staged=%q transaction=%q proposal=%q publicNote=%v", stagedNote, note, proposalNote, public[0]["note"])
			}
		})
	}
}
