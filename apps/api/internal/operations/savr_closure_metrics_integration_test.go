package operations

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// UISC-02: the three SAVR closure metrics must be measurable from stored state,
// must ignore missing provenance instead of counting it as zero, and must not
// treat an independent evidence check as a semantic re-decision.
func TestSavrClosureMetricsAreMeasurableAndHistoricallyHonest(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var household, source string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("SAVR closure %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("savr-closure-%d", stamp)).Scan(&source); err != nil {
		t.Fatal(err)
	}
	// Eligible and validator-induced: the validation re-asks `amount_idr`, which it
	// already accepted at the boundary.
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','RESOLVED',now(),$3::jsonb)`, household, source, `{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","decisionClass":"EVIDENCE_GAP","validationConsequence":"BOUNDED_RESIDUAL","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["amount_idr"],"affectedFacts":["amount_idr"],"decisionProvenance":{"accepted_dimensions_at_validation":["amount_idr"]}}`); err != nil {
		t.Fatal(err)
	}
	// Eligible and faithful: validation asks for a fact it did not accept,
	// explained by the bounded-residual consequence.
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','RESOLVED',now(),$3::jsonb)`, household, source, `{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","decisionClass":"EVIDENCE_GAP","validationConsequence":"BOUNDED_RESIDUAL","whyNotAutoConfirm":"x","knownFacts":{"amount_idr":"54000"},"missingFacts":["transaction_semantics"],"affectedFacts":["transaction_semantics"],"decisionProvenance":{"accepted_dimensions_at_validation":["amount_idr"]}}`); err != nil {
		t.Fatal(err)
	}
	// Historical row with no contract: unknown, must not enter either denominator.
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','RESOLVED',now())`, household, source); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO intelligence_phase_telemetry(household_id,capability,purpose,semantic_dimensions,answered_dimensions,latency_ms,outcome,accepted_dimensions_at_entry) VALUES
		($1,'JEV','RESIDUAL_CATEGORY',ARRAY['category'],ARRAY['category'],5,'SUCCEEDED',ARRAY[]::text[]),
		($1,'JEV','RESIDUAL_CATEGORY',ARRAY['category'],ARRAY['category'],5,'SUCCEEDED',ARRAY['category']),
		($1,'JEV','EVIDENCE_SUPPORT',ARRAY['amount_supported'],ARRAY['amount_supported'],5,'SUCCEEDED',ARRAY['amount_supported']),
		($1,'JEV','OTHER_BOUNDED',ARRAY['same_real_event'],ARRAY['same_real_event'],5,'SUCCEEDED',ARRAY['same_real_event']),
		($1,'JEV','RESIDUAL_CATEGORY',ARRAY['category'],ARRAY['category'],5,'SUCCEEDED',NULL),
		($1,'GENERATIVE','EXTRACTION',ARRAY['amount'],ARRAY['amount'],5,'SUCCEEDED',NULL)`, household); err != nil {
		t.Fatal(err)
	}
	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, household)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ValidatorEligibleReviews != 2 || aggregate.ValidatorInducedReviews != 1 || aggregate.ValidatorInducedRate != 0.5 || aggregate.ValidatorUnknownReviews != 1 {
		t.Fatalf("validator-induced: eligible=%d induced=%d rate=%v unknown=%d", aggregate.ValidatorEligibleReviews, aggregate.ValidatorInducedReviews, aggregate.ValidatorInducedRate, aggregate.ValidatorUnknownReviews)
	}
	if aggregate.SemanticEligiblePhases != 2 || aggregate.SemanticReDecisions != 1 || aggregate.SemanticReDecisionRate != 0.5 || aggregate.SemanticUnknownPhases != 1 {
		t.Fatalf("semantic re-decision: eligible=%d redecided=%d rate=%v unknown=%d", aggregate.SemanticEligiblePhases, aggregate.SemanticReDecisions, aggregate.SemanticReDecisionRate, aggregate.SemanticUnknownPhases)
	}
	if aggregate.ResidualEligibleReviews != 2 || aggregate.ResidualViolations != 0 || aggregate.ResidualFidelityRate != 1 || aggregate.ResidualUnknownReviews != 1 {
		t.Fatalf("residual fidelity: eligible=%d violations=%d rate=%v unknown=%d", aggregate.ResidualEligibleReviews, aggregate.ResidualViolations, aggregate.ResidualFidelityRate, aggregate.ResidualUnknownReviews)
	}
	if len(aggregate.Coverage) != 1 || aggregate.Coverage[0] != "pre_migration_telemetry_history" || len(aggregate.CoverageIncomplete) != 3 {
		t.Fatalf("only historical coverage may remain incomplete: %+v / %+v", aggregate.Coverage, aggregate.CoverageIncomplete)
	}
}

func TestResidualContractFidelityFlagsUndeclaredHumanWork(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var household, source string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("SAVR fidelity %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("savr-fidelity-%d", stamp)).Scan(&source); err != nil {
		t.Fatal(err)
	}
	seed := func(reason, decision, action string, fields []string) {
		t.Helper()
		var item string
		if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,resolution_action,decision) VALUES($1,$2,$3,'RESOLVED',now(),NULLIF($4,''),$5::jsonb) RETURNING id`, household, source, reason, action, decision).Scan(&item); err != nil {
			t.Fatal(err)
		}
		if len(fields) > 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,review_item_id,event_type,action,changed_fields) VALUES($1,$2,'REVIEW_TURN',$3,$4)`, household, item, action, fields); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("AMBIGUOUS_CATEGORY", `{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","whyNotAutoConfirm":"x","knownFacts":{"category":"food"},"missingFacts":["category"]}`, "", nil)
	seed("AMBIGUOUS_CATEGORY", `{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["category"]}`, "CONFIRM_REVIEW", []string{"amount"})
	seed("MISSING_PAY_DATE", `{"version":1,"reasonCode":"MISSING_PAY_DATE","decisionClass":"EVIDENCE_GAP","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["category"]}`, "SET_PAY_DATE", nil)
	seed("RECEIPT_MISMATCH", `{"version":1,"reasonCode":"RECEIPT_MISMATCH","decisionClass":"CORRECTION_CONFIRMATION","validationConsequence":"QUALITY_SIGNAL","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["amount"]}`, "", nil)
	seed("MANUAL_CORRECTION", `{"version":1,"reasonCode":"MANUAL_CORRECTION","decisionClass":"CORRECTION_CONFIRMATION","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["correction_details"]}`, "CONFIRM_REVIEW", []string{"amount"})
	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, household)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ResidualEligibleReviews != 5 || aggregate.ResidualViolations != 4 || aggregate.ResidualFidelityRate != 0.2 || aggregate.ResidualUnknownReviews != 0 {
		t.Fatalf("residual fidelity: eligible=%d violations=%d rate=%v unknown=%d", aggregate.ResidualEligibleReviews, aggregate.ResidualViolations, aggregate.ResidualFidelityRate, aggregate.ResidualUnknownReviews)
	}
}
