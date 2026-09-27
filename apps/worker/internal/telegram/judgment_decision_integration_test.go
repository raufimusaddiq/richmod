package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// A batch with an unresolved category must not offer confirmation as if every
// semantic dimension had already been accepted.
func TestPendingBatchConfirmationRejectsMissingCategory(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-authority")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)
	items := `[{"Type":"EXPENSE","Amount":"50000","Merchant":"makan siang","CategorySlug":"","Description":"makan siang","TransactionAt":"` + f.state.Now.Format(time.RFC3339) + `"}]`
	_, err = f.pool.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status) VALUES($1,$2,$3,$4,$5::jsonb,'PENDING')`, f.householdID, f.chatID, f.chatID, f.sourceID, items)
	mustAgentTest(t, err)

	processor := NewProcessor(f.pool, nil)
	handled, err := processor.processPendingBatch(ctx, f.householdID, f.update, f.sourceID, "ya")
	if err == nil {
		t.Fatal("missing category must block confirmation")
	}
	if !handled {
		t.Fatal("confirming a pending batch must be handled")
	}
	var confirmed int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID).Scan(&confirmed))
	if confirmed != 0 {
		t.Fatalf("unresolved category must not confirm a batch; confirmed=%d", confirmed)
	}
	var decisionRows int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE household_id=$1`, f.householdID).Scan(&decisionRows))
	if decisionRows != 0 {
		t.Fatalf("an unresolved batch must not record a decision; rows=%d", decisionRows)
	}
}

// A complete staged batch is confirmed by the user's explicit action, without
// a redundant bounded semantic vote or fabricated judgment provenance.
func TestPendingBatchConfirmationUsesHumanAuthority(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-decisions")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)
	at := f.state.Now.Format(time.RFC3339)
	items := `[{"Type":"EXPENSE","Amount":"50000","Merchant":"makan siang","CategorySlug":"dining","Description":"makan siang","TransactionAt":"` + at + `"},{"Type":"INCOME","Amount":"90000","Merchant":"bonus","CategorySlug":"","Description":"bonus","TransactionAt":"` + at + `"}]`
	_, err = f.pool.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status) VALUES($1,$2,$3,$4,$5::jsonb,'PENDING')`, f.householdID, f.chatID, f.chatID, f.sourceID, items)
	mustAgentTest(t, err)

	processor := NewProcessor(f.pool, nil)
	handled, err := processor.processPendingBatch(ctx, f.householdID, f.update, f.sourceID, "ya")
	mustAgentTest(t, err)
	if !handled {
		t.Fatal("confirming a pending batch must be handled")
	}
	var confirmed, decisionRows int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID).Scan(&confirmed))
	if confirmed != 2 {
		t.Fatalf("confirmed=%d; want 2", confirmed)
	}
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE household_id=$1`, f.householdID).Scan(&decisionRows))
	if decisionRows != 0 {
		t.Fatalf("human confirmation must not forge bounded decisions; rows=%d", decisionRows)
	}
	var audits int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='CONFIRM_PENDING_BATCH'`, f.householdID).Scan(&audits))
	if audits != 2 {
		t.Fatalf("human confirmation audit rows=%d; want 2", audits)
	}
}

// Every Jev-influenced mutation must be explainable from bounded provenance:
// model version, policy version, question keys, bounded answers, and outcome
// must be persisted in the same transaction as the canonical write (PRD §15/§16),
// and no raw user text may be stored there.
func TestJudgmentDecisionProvenanceIsRecordedWithTheMutation(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "decision-provenance")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)

	processor := &Processor{pool: f.pool}
	decision := TransactionSemanticDecision{
		RouteAccepted: true, TransactionType: "EXPENSE", TypeAccepted: true,
		AmountSupported: true, DateSupported: true, CategorySlug: "dining", CategoryAccepted: true,
		AmbiguityDecidedNotAmbiguous: true,
		DecisionSource:               "JEV", Model: "jev-test", PolicyVersion: judgmentPolicyVersion,
	}
	value := validatedExtraction{Type: "EXPENSE", Amount: "50000", TransactionAt: f.state.Now, Merchant: "makan siang", CategorySlug: "dining"}
	mustAgentTest(t, processor.persistTransaction(ctx, f.sourceID, f.householdID, f.update, value, gateway.Metadata{Model: "extract-test"}, decision))

	var (
		task, model, policyVersion, outcome, summary string
		questionKeys                                 []string
	)
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT task,COALESCE(model,''),policy_version,outcome,answer_summary_json::text,question_keys FROM judgment_decision WHERE household_id=$1 AND source_event_id=$2`, f.householdID, f.sourceID).Scan(&task, &model, &policyVersion, &outcome, &summary, &questionKeys))
	if task != "TRANSACTION_SEMANTICS" || outcome != "CONFIRMED" {
		t.Fatalf("task=%s outcome=%s; want TRANSACTION_SEMANTICS/CONFIRMED", task, outcome)
	}
	if model != "jev-test" || policyVersion != judgmentPolicyVersion {
		t.Fatalf("model=%s policy=%s; want the configured model and policy version", model, policyVersion)
	}
	if len(questionKeys) == 0 {
		t.Fatal("question keys must be recorded for audit")
	}
	if contains([]string{summary}, "makan siang") {
		t.Fatalf("provenance must not store raw message content: %s", summary)
	}
	var transactionStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, f.householdID).Scan(&transactionStatus))
	if transactionStatus != "CONFIRMED" {
		t.Fatalf("transaction status=%s; want CONFIRMED", transactionStatus)
	}
}

// A denied decision must still leave provenance, and must not confirm the
// transaction: the audit trail explains the Review outcome.
func TestDeniedDecisionRecordsReviewOutcome(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "decision-review")
	processor := &Processor{pool: f.pool}
	value := validatedExtraction{Type: "EXPENSE", Amount: "50000", TransactionAt: f.state.Now, Merchant: "makan siang"}
	decision := TransactionSemanticDecision{PolicyVersion: judgmentPolicyVersion, DecisionSource: "JUDGMENT_UNAVAILABLE"}
	mustAgentTest(t, processor.persistTransaction(ctx, f.sourceID, f.householdID, f.update, value, gateway.Metadata{}, decision))

	var outcome string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT outcome FROM judgment_decision WHERE household_id=$1 AND source_event_id=$2`, f.householdID, f.sourceID).Scan(&outcome))
	if outcome != "NEEDS_REVIEW" {
		t.Fatalf("outcome=%s; want NEEDS_REVIEW", outcome)
	}
	var transactionStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, f.householdID).Scan(&transactionStatus))
	if transactionStatus != "NEEDS_REVIEW" {
		t.Fatalf("transaction status=%s; want NEEDS_REVIEW", transactionStatus)
	}
}
