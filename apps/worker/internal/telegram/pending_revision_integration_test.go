package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestPendingButtonsCannotAuthorizeReplacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, kind := range []string{"action", "batch"} {
		for _, choice := range []string{"yes", "no"} {
			t.Run(kind+"_"+choice, func(t *testing.T) {
				f := newAgentIntegrationFixture(t, "pending-revision")
				p := NewProcessor(f.pool, nil)
				stage := func(label string) agentToolResult {
					t.Helper()
					var result agentToolResult
					var err error
					if kind == "action" {
						_, err = f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000,'IDR',now(),$2,now())`, f.householdID, label)
						mustAgentTest(t, err)
						result, _, err = p.agentStageCorrection(ctx, f.state, gateway.ToolCall{}, map[string]any{"search_text": label, "description": "corrected-" + label, "period": "TODAY"})
					} else {
						result, _, err = p.agentStageBatch(ctx, f.state, gateway.ToolCall{}, map[string]any{"items": []any{map[string]any{
							"type": "INCOME", "amount_idr": "1000", "description": label,
							"date_reference": "TODAY", "date_provenance": "USER_STATED", "confidence": 1.0,
						}}})
					}
					mustAgentTest(t, err)
					if result.Status != "PENDING_CONFIRMATION" || result.confirmationToken == "" {
						t.Fatalf("staging did not return a revision: %+v", result)
					}
					return result
				}
				a := stage("synthetic-a")
				var rowA, rowB string
				table := "telegram_pending_" + kind
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM `+table+` WHERE household_id=$1 AND status='PENDING'`, f.householdID).Scan(&rowA))
				b := stage("synthetic-b")
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM `+table+` WHERE household_id=$1 AND status='PENDING'`, f.householdID).Scan(&rowB))
				if rowA != rowB || a.confirmationToken == b.confirmationToken {
					t.Fatal("UPSERT must rotate the revision even when the row ID is unchanged")
				}
				prefix := "pending:" + kind + ":"
				for _, stale := range []string{prefix + "yes:" + a.confirmationToken, prefix + "no:" + a.confirmationToken, prefix + "yes", prefix + "no"} {
					handled, err := p.processPendingConfirmCallback(ctx, f.sourceID, f.householdID, f.update, stale)
					mustAgentTest(t, err)
					if !handled {
						t.Fatal("stale callback escaped its deterministic handler")
					}
				}
				var status string
				var changed int
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM `+table+` WHERE id=$1`, rowB).Scan(&status))
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1`, f.householdID).Scan(&changed))
				if status != "PENDING" || changed != 0 {
					t.Fatalf("stale callback changed current work: status=%s audits=%d", status, changed)
				}
				for _, invalid := range []string{"household", "user", "chat", "expired"} {
					hh, update := f.householdID, f.update
					switch invalid {
					case "household":
						hh = "00000000-0000-0000-0000-000000000000"
					case "user":
						update.Message.From.ID++
					case "chat":
						update.Message.Chat.ID++
					case "expired":
						_, err := f.pool.Exec(ctx, `UPDATE `+table+` SET expires_at=now()-interval '1 second' WHERE id=$1`, rowB)
						mustAgentTest(t, err)
					}
					var handled bool
					var err error
					if kind == "action" {
						handled, err = p.processPendingEdit(ctx, hh, update, f.sourceID, true, b.confirmationToken)
					} else {
						handled, err = p.processPendingBatch(ctx, hh, update, f.sourceID, true, b.confirmationToken)
					}
					mustAgentTest(t, err)
					if handled {
						t.Fatalf("%s token was authorized", invalid)
					}
				}
				_, err := f.pool.Exec(ctx, `UPDATE `+table+` SET expires_at=now()+interval '5 minutes' WHERE id=$1`, rowB)
				mustAgentTest(t, err)
				for i := 0; i < 2; i++ {
					_, err := p.processPendingConfirmCallback(ctx, f.sourceID, f.householdID, f.update, prefix+choice+":"+b.confirmationToken)
					mustAgentTest(t, err)
				}
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM `+table+` WHERE id=$1`, rowB).Scan(&status))
				want := "CANCELLED"
				if choice == "yes" {
					want = "CONFIRMED"
				}
				if status != want {
					t.Fatalf("current revision status=%s want=%s", status, want)
				}
				var total, correctedA, postedB, audits int
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE description='corrected-synthetic-a'),count(*) FILTER(WHERE description IN ('corrected-synthetic-b','synthetic-b')) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&total, &correctedA, &postedB))
				mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1`, f.householdID).Scan(&audits))
				wantTotal, wantAudits := 0, 0
				if kind == "action" {
					wantTotal = 2
				}
				if choice == "yes" {
					wantAudits = 1
					if kind == "batch" {
						wantTotal = 1
					}
				}
				if total != wantTotal || correctedA != 0 || audits != wantAudits || (choice == "yes" && postedB != 1) {
					t.Fatalf("wrong target or replay: total=%d correctedA=%d postedB=%d audits=%d", total, correctedA, postedB, audits)
				}
			})
		}
	}
}

func TestPendingBatchEditRotatesConfirmation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-edit-revision")
	p := NewProcessor(f.pool, nil)
	staged, _, err := p.agentStageBatch(ctx, f.state, gateway.ToolCall{}, map[string]any{"items": []any{map[string]any{
		"type": "INCOME", "amount_idr": "1000", "description": "synthetic-original",
		"date_reference": "TODAY", "date_provenance": "USER_STATED", "confidence": 1.0,
	}}})
	mustAgentTest(t, err)
	description := "synthetic-patched"
	edited, _, err := p.agentFinalizePendingBatch(ctx, f.state, gateway.ToolCall{}, false, &pendingBatchPatch{Index: 0, Description: &description})
	mustAgentTest(t, err)
	if edited.Status != "UPDATED" || edited.confirmationToken == "" || edited.confirmationToken == staged.confirmationToken || agentPendingMarkup([]agentToolResult{edited}) == nil {
		t.Fatal("batch edit must rotate and render the new revision")
	}
	for _, confirm := range []bool{true, false} {
		handled, err := p.processPendingBatch(ctx, f.householdID, f.update, f.sourceID, confirm, staged.confirmationToken)
		mustAgentTest(t, err)
		if handled {
			t.Fatal("pre-edit button authorized edited facts")
		}
	}
	handled, err := p.processPendingBatch(ctx, f.householdID, f.update, f.sourceID, true, edited.confirmationToken)
	mustAgentTest(t, err)
	if !handled {
		t.Fatal("new revision did not authorize its batch")
	}
	var total int
	var got string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*),min(description) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&total, &got))
	if total != 1 || got != description {
		t.Fatalf("posted batch total=%d description=%s", total, got)
	}
}
