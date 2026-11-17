package operations

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestProductAggregateRHICEBindsMergeAndFinancialEmailEvidencePerHousehold pins
// the RHICE rewrite: a MERGE_REVIEW binding (resolution_values->>'transaction_id')
// and a financial-email-observation binding (feo.transaction_id=te.transaction_id)
// both count, and a second household with identical data never bleeds in. The
// old UNION-of-three-bindings query is run inline and must agree.
func TestProductAggregateRHICEBindsMergeAndFinancialEmailEvidencePerHousehold(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()

	seedHousehold := func(label string) string {
		var householdID string
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("%s %d", label, stamp)).Scan(&householdID); err != nil {
			t.Fatal(err)
		}
		return householdID
	}
	seedEvent := func(householdID, label string) string {
		var eventID string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode(md5($2),'hex'),'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("%s-%d", label, stamp)).Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		return eventID
	}
	seedTransaction := func(householdID string) string {
		var txID string
		if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',1000,now(),now()) RETURNING id`, householdID).Scan(&txID); err != nil {
			t.Fatal(err)
		}
		return txID
	}
	seedReview := func(householdID, sourceEventID, reviewType, resolutionAction, resolutionValues string, feoID *string) string {
		var reviewID string
		if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,financial_email_observation_id,review_type,status,resolution_action,resolution_values,resolved_at,decision) VALUES($1,$2,$3,$4,'RESOLVED',$5,$6::jsonb,now(),'{"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW"]}'::jsonb) RETURNING id`, householdID, sourceEventID, feoID, reviewType, resolutionAction, resolutionValues).Scan(&reviewID); err != nil {
			t.Fatal(err)
		}
		return reviewID
	}
	seedTurn := func(householdID, reviewID string) {
		if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,review_item_id,event_type,action,changed_fields,bounded_choices,occurred_at) VALUES($1,$2,'REVIEW_TURN','CONFIRM_REVIEW',ARRAY['category'],0,now())`, householdID, reviewID); err != nil {
			t.Fatal(err)
		}
	}

	seed := func(label string) string {
		householdID := seedHousehold(label)
		// MERGE_REVIEW binding: resolution_values->>'transaction_id' points at an in-window transaction.
		mergeEvent := seedEvent(householdID, label+"-merge")
		mergeTx := seedTransaction(householdID)
		mergeReview := seedReview(householdID, mergeEvent, "POSSIBLE_DUPLICATE", "MERGE_REVIEW", fmt.Sprintf(`{"transaction_id":%q}`, mergeTx), nil)
		seedTurn(householdID, mergeReview)
		// Financial-email-observation binding: feo.transaction_id = te.transaction_id.
		feoEvent := seedEvent(householdID, label+"-feo")
		feoTx := seedTransaction(householdID)
		var feoID string
		if err := pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status,transaction_id) VALUES($1,$2,0,'CASH_MOVEMENT','{}'::jsonb,'APPLIED',$3) RETURNING id`, householdID, feoEvent, feoTx).Scan(&feoID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TEST')`, feoTx, feoEvent); err != nil {
			t.Fatal(err)
		}
		feoReview := seedReview(householdID, feoEvent, "UNKNOWN_EMAIL_TEMPLATE", "CONFIRM_REVIEW", `{}`, &feoID)
		seedTurn(householdID, feoReview)
		return householdID
	}

	householdID := seed("RHICE binding")
	seed("RHICE binding other")

	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ExplicitInputs != 2 || aggregate.TypedFields != 2 || aggregate.CanonicalEvents != 2 {
		t.Fatalf("household 1 RHICE = inputs %d, typed %d, events %d; want 2, 2, 2", aggregate.ExplicitInputs, aggregate.TypedFields, aggregate.CanonicalEvents)
	}

	// The old UNION-of-three-bindings query must agree with the rewrite.
	var oldInputs, oldTyped, oldOpen, oldAccepted int
	if err := pool.QueryRow(ctx, `
		WITH recent_transactions AS (
		  SELECT id FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND created_at >= now() - interval '30 days'
		), stale_transactions AS (
		  SELECT DISTINCT transaction_id FROM review_item
		  WHERE household_id=$1 AND transaction_id IS NOT NULL AND status='RESOLVED'
		    AND resolved_at < now() - interval '30 days'
		), review_events AS (
		  SELECT DISTINCT ri.id,ri.resolution_action
		  FROM review_item ri
		  JOIN recent_transactions t ON ri.transaction_id=t.id
		    OR (ri.resolution_action='MERGE_REVIEW' AND ri.resolution_values->>'transaction_id'=t.id::text)
		    OR EXISTS (
		      SELECT 1 FROM transaction_evidence te
		      WHERE te.transaction_id=t.id AND (
		        (ri.financial_email_observation_id IS NOT NULL AND EXISTS (SELECT 1 FROM financial_email_observation feo WHERE feo.id=ri.financial_email_observation_id AND feo.transaction_id=t.id))
		        OR (ri.wealth_observation_id IS NOT NULL AND te.metadata_json->>'observation_id'=ri.wealth_observation_id::text)
		        OR (ri.financial_email_observation_id IS NULL AND te.source_event_id=COALESCE(
		          ri.source_event_id,
		          (SELECT source_event_id FROM transaction_proposal WHERE id=ri.proposal_id),
		          (SELECT source_event_id FROM document WHERE id=ri.document_id)))
		      )
		    )
		  WHERE ri.household_id=$1 AND ri.status='RESOLVED' AND ri.resolved_at >= now() - interval '30 days'
		    AND (ri.transaction_id IS NULL OR ri.transaction_id NOT IN (SELECT transaction_id FROM stale_transactions))
		), human_turns AS (
		  SELECT COALESCE(sum(cardinality(e.changed_fields)+e.bounded_choices),0) AS inputs,
		         COALESCE(sum(cardinality(e.changed_fields)),0) AS typed_fields
		  FROM product_telemetry_event e
		  JOIN review_events re ON re.id=e.review_item_id
		  WHERE e.household_id=$1 AND e.event_type='REVIEW_TURN' AND e.occurred_at >= now() - interval '30 days'
		    AND e.action NOT IN ('IGNORE','EMAIL_RECEIVED_AT_FALLBACK','RECONCILED_TERMINAL_TRANSACTION','LEGACY_TRANSACTION_RESOLVED','NO_LONGER_APPLICABLE')
		)
		SELECT
		 (SELECT inputs FROM human_turns),
		 (SELECT typed_fields FROM human_turns),
		 (SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')),
		 count(*) FILTER (WHERE resolution_action IN (
		   'CONFIRM_REVIEW','TELEGRAM_CONFIRMED','TELEGRAM_MERCHANT_DECISION'))
		FROM review_events`, householdID).Scan(&oldInputs, &oldTyped, &oldOpen, &oldAccepted); err != nil {
		t.Fatal(err)
	}
	if oldInputs != aggregate.ExplicitInputs || oldTyped != aggregate.TypedFields || oldOpen != aggregate.OpenReviews || oldAccepted != aggregate.AcceptedWithoutEdit {
		t.Fatalf("old UNION query disagrees: old=(%d,%d,%d,%d) new=(%d,%d,%d,%d)", oldInputs, oldTyped, oldOpen, oldAccepted, aggregate.ExplicitInputs, aggregate.TypedFields, aggregate.OpenReviews, aggregate.AcceptedWithoutEdit)
	}
}
