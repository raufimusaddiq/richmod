package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// batchAffirmationEngine affirms every bounded transaction question so a batch
// confirmation can be attributed to the decision plane instead of model
// self-confidence.
type batchAffirmationEngine struct{ t *testing.T }

func (e batchAffirmationEngine) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	if _, ok := request.Questions["transaction_type"]; !ok {
		e.t.Fatalf("batch confirmation must ask the shared transaction bundle, got %v", request.Questions)
	}
	answers := map[string]judgment.Answer{
		"amount_support":     decidedNoul(0.99),
		"date_support":       decidedNoul(0.99),
		"material_ambiguity": decidedNoul(0.02),
	}
	if question, ok := request.Questions["category"]; ok {
		criteria, valid := question.Criteria.(map[string]any)
		if !valid {
			e.t.Fatalf("expected category criteria, got %T", question.Criteria)
		}
		answers["category"] = confidentChoice(criteria, "dining")
	}
	// The INCOME item has no category question, so the type answer must be
	// derived from the hint the batch passed in.
	typeHint := ""
	if state, ok := request.State.(map[string]any); ok {
		typeHint, _ = state["transaction_type_hint"].(string)
	}
	if typeHint == "" {
		typeHint = "EXPENSE"
	}
	answers["transaction_type"] = confidentChoice(judgmentTypeCriteria, typeHint)
	return judgment.Result{Model: "jev-batch", Answers: answers}, nil
}

// A pending batch must not be confirmable through a different authority than a
// single transaction. With no judgment plane configured the confirmation must
// fail closed, leave the batch unresolvable-by-write, and create no canonical
// transaction (ADR-038; Hermes review on PR #98).
func TestPendingBatchConfirmationFailsClosedWithoutDecisionPlane(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-authority")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)
	items := `[{"Type":"EXPENSE","Amount":"50000","Merchant":"makan siang","CategorySlug":"dining","Description":"makan siang","TransactionAt":"` + f.state.Now.Format(time.RFC3339) + `"}]`
	_, err = f.pool.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status) VALUES($1,$2,$3,$4,$5::jsonb,'PENDING')`, f.householdID, f.chatID, f.chatID, f.sourceID, items)
	mustAgentTest(t, err)

	processor := NewProcessor(f.pool, nil)
	handled, err := processor.processPendingBatch(ctx, f.householdID, f.update, f.sourceID, "ya")
	mustAgentTest(t, err)
	if !handled {
		t.Fatal("confirming a pending batch must be handled")
	}
	var confirmed int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID).Scan(&confirmed))
	if confirmed != 0 {
		t.Fatalf("unavailable judgment plane must not confirm a batch; confirmed=%d", confirmed)
	}
	var decisionRows int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE household_id=$1`, f.householdID).Scan(&decisionRows))
	if decisionRows != 0 {
		t.Fatalf("a fail-closed batch must not record a confirmed decision; rows=%d", decisionRows)
	}
}

// With an affirmative bounded evaluation the batch confirms and leaves one
// bounded provenance row per canonical transaction.
func TestPendingBatchConfirmationRecordsOneDecisionPerItem(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "batch-decisions")
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining')`, f.householdID)
	mustAgentTest(t, err)
	at := f.state.Now.Format(time.RFC3339)
	items := `[{"Type":"EXPENSE","Amount":"50000","Merchant":"makan siang","CategorySlug":"dining","Description":"makan siang","TransactionAt":"` + at + `"},{"Type":"INCOME","Amount":"90000","Merchant":"bonus","CategorySlug":"","Description":"bonus","TransactionAt":"` + at + `"}]`
	_, err = f.pool.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status) VALUES($1,$2,$3,$4,$5::jsonb,'PENDING')`, f.householdID, f.chatID, f.chatID, f.sourceID, items)
	mustAgentTest(t, err)

	processor := NewProcessor(f.pool, nil)
	processor.SetJudgment(batchAffirmationEngine{t: t})
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
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE household_id=$1 AND outcome='CONFIRMED'`, f.householdID).Scan(&decisionRows))
	if decisionRows != 2 {
		t.Fatalf("decision rows=%d; want one per confirmed item", decisionRows)
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
