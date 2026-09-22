package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

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
		DecisionSource: "JEV", Model: "jev-test", PolicyVersion: judgmentPolicyVersion,
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
