package telegram

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func seedAttributionReview(t *testing.T, kind, reason string, dateKnown bool) (agentIntegrationFixture, string, string, string) {
	t.Helper()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "audit-attribution")
	var transaction, proposal, request string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,$2,'NEEDS_REVIEW',100,'2026-10-01T00:00:00+07:00') RETURNING id`, f.householdID, kind).Scan(&transaction))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,100,'2026-10-01T00:00:00+07:00',.9,'NEEDS_REVIEW',jsonb_build_object('date_known',$4::boolean)) RETURNING id`, f.householdID, f.sourceID, kind, dateKnown).Scan(&proposal))
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',jsonb_build_object('proposal_id',$3::uuid))`, transaction, f.sourceID, proposal)
	mustAgentTest(t, err)
	decision, _ := reviewdec.Preset(reason, "transaction", transaction)
	tx, err := f.pool.Begin(ctx)
	mustAgentTest(t, err)
	mustAgentTest(t, EnqueueReviewRequest(ctx, tx, transaction, reason, f.chatID, 0, "Synthetic review", decision))
	mustAgentTest(t, tx.Commit(ctx))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, transaction).Scan(&request))
	mustAgentTest(t, NewProcessor(f.pool, nil).BindReviewMessage(ctx, request, f.chatID, 99, ""))
	return f, transaction, proposal, request
}

func TestAuditF05DistinctDuplicateRequiresCategory(t *testing.T) {
	ctx := context.Background()
	f, transaction, proposal, _ := seedAttributionReview(t, "EXPENSE", "POSSIBLE_DUPLICATE", true)
	raw, err := json.Marshal(callbackUpdate(f.chatID, 99, "review:dup:new"))
	mustAgentTest(t, err)
	mustAgentTest(t, NewProcessor(f.pool, nil).Process(ctx, f.seedSourceEvent(ctx, t, "distinct", raw)))
	var status, proposalStatus, reviewStatus string
	var missingCategory bool
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,t.category_id IS NULL,p.proposal_status,ri.status FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id JOIN transaction_proposal p ON p.id=$2 WHERE t.id=$1`, transaction, proposal).Scan(&status, &missingCategory, &proposalStatus, &reviewStatus))
	if status != "NEEDS_REVIEW" || !missingCategory || proposalStatus != "NEEDS_REVIEW" || (reviewStatus != "OPEN" && reviewStatus != "PENDING_SEND") {
		t.Fatalf("F05: transaction=%s categoryMissing=%v proposal=%s review=%s; distinct choice must retain category review", status, missingCategory, proposalStatus, reviewStatus)
	}
	var category string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Synthetic category','synthetic') RETURNING id`, f.householdID).Scan(&category))
	raw, err = json.Marshal(callbackUpdate(f.chatID, 99, "review:cat:"+category))
	mustAgentTest(t, err)
	mustAgentTest(t, NewProcessor(f.pool, nil).Process(ctx, f.seedSourceEvent(ctx, t, "distinct-category", raw)))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transaction).Scan(&status))
	if status != "CONFIRMED" {
		t.Fatalf("category continuation left transaction=%s", status)
	}
}

func TestAuditF06DateThenCategoryCompletes(t *testing.T) {
	ctx := context.Background()
	f, transaction, proposal, request := seedAttributionReview(t, "EXPENSE", "TRANSACTION_FACTS_MISSING", false)
	var category string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Synthetic category','synthetic') RETURNING id`, f.householdID).Scan(&category))
	p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","transaction_at":"2026-10-03"}`})
	sendTypedReviewReply(t, ctx, f, p, request, "2026-10-03", true)
	var residualDate, dateKnown bool
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT ri.decision->'missingFacts' ? 'transaction_at',COALESCE((p.metadata_json->>'date_known')::boolean,false) FROM review_item ri,transaction_proposal p WHERE ri.transaction_id=$1 AND p.id=$2`, transaction, proposal).Scan(&residualDate, &dateKnown))
	raw, err := json.Marshal(callbackUpdate(f.chatID, 99, "review:cat:"+category))
	mustAgentTest(t, err)
	mustAgentTest(t, p.Process(ctx, f.seedSourceEvent(ctx, t, "category", raw)))
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transaction).Scan(&status))
	if residualDate || !dateKnown || status != "CONFIRMED" {
		t.Fatalf("F06: residualDate=%v dateKnown=%v transaction=%s; accepted date must survive category continuation", residualDate, dateKnown, status)
	}
}

func TestAuditF07IncomeCannotConfirmFallbackDate(t *testing.T) {
	ctx := context.Background()
	f, transaction, _, request := seedAttributionReview(t, "INCOME", "TRANSFER_CLASSIFICATION", false)
	raw, err := json.Marshal(callbackUpdate(f.chatID, 99, "review:income"))
	mustAgentTest(t, err)
	mustAgentTest(t, NewProcessor(f.pool, nil).Process(ctx, f.seedSourceEvent(ctx, t, "income", raw)))
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transaction).Scan(&status))
	if status != "NEEDS_REVIEW" {
		t.Fatalf("F07: transaction=%s; unchosen fallback date must remain in review", status)
	}
	p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","transaction_at":"2026-10-03"}`})
	sendTypedReviewReply(t, ctx, f, p, request, "2026-10-03", true)
	var date string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,to_char(transaction_at AT TIME ZONE 'Asia/Jakarta','YYYY-MM-DD') FROM transaction WHERE id=$1`, transaction).Scan(&status, &date))
	if status != "CONFIRMED" || date != "2026-10-03" {
		t.Fatalf("income date continuation: status=%s date=%s", status, date)
	}
}
