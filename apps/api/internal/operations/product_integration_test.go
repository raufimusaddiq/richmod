package operations

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestProductAggregateReportsReviewRatesBySourceAndReason(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Product Aggregate %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	seedEvent := func(sourceType, status, label string) string {
		var eventID string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),decode(md5($3),'hex'),$4) RETURNING id`, householdID, sourceType, fmt.Sprintf("%s-%d", label, stamp), status).Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		return eventID
	}
	// Two reviewed events (one bank, one telegram) plus two terminal events. The
	// reviewed events are still NEEDS_REVIEW, so they are also part of the cohort:
	// human-touch rate is distinct reviewed events (2) over all source events (4).
	// A denominator of only terminal events would let the ratio exceed 1.
	bankEventID := seedEvent("BANK_EMAIL", "NEEDS_REVIEW", "product-bank")
	telegramEventID := seedEvent("TELEGRAM_TEXT", "NEEDS_REVIEW", "product-telegram")
	seedEvent("TELEGRAM_TEXT", "PROCESSED", "product-terminal-a")
	seedEvent("BANK_EMAIL", "IGNORED", "product-terminal-b")
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN')`, householdID, bankEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN')`, householdID, telegramEventID); err != nil {
		t.Fatal(err)
	}
	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.SourceEvents != 4 || aggregate.Processed != 1 || aggregate.Ignored != 1 || aggregate.NeedsReview != 2 {
		t.Fatalf("unexpected product aggregate: %+v", aggregate)
	}
	if aggregate.ReviewedEvents != 2 || aggregate.HumanTouchRate != 0.5 {
		t.Fatalf("human-touch rate must count distinct reviewed events: %+v", aggregate)
	}
	if aggregate.BySource["BANK_EMAIL"] != 2 || aggregate.ReviewBySource["BANK_EMAIL"] != 1 || aggregate.ReviewBySource["TELEGRAM_TEXT"] != 1 || aggregate.ReviewByReason["AMBIGUOUS_CATEGORY"] != 1 || aggregate.ReviewByReason["UNKNOWN_MERCHANT"] != 1 {
		t.Fatalf("source/reason counts missing: %+v", aggregate)
	}
	// Fewer terminal events than reviewed events must not push the rate above 1:
	// both sides use the whole window cohort.
	if aggregate.HumanTouchRate > 1 {
		t.Fatalf("human-touch rate must stay bounded: %+v", aggregate)
	}
	// PRD section 22: RHICE is explicit inputs over canonical events, derived from
	// resolution rows and transactions rather than written by a new pipeline. The
	// two open reviews above are friction but not yet inputs, so they must not
	// inflate the numerator.
	if aggregate.ExplicitInputs != 0 || aggregate.RHICE != 0 {
		t.Fatalf("open reviews are not explicit inputs: %+v", aggregate)
	}
	if aggregate.OpenReviews != 2 {
		t.Fatalf("open reviews must be counted as outstanding friction: %+v", aggregate)
	}

	// A transaction in the window is the RHICE denominator cohort. Each review
	// below is bound to one so the numerator and denominator share a cohort.
	seedTx := func() string {
		var txID string
		if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR',now(),'product aggregate',now()) RETURNING id`, householdID).Scan(&txID); err != nil {
			t.Fatal(err)
		}
		return txID
	}

	// An IGNORE resolves a review without producing a canonical event, so it must
	// not count as an explicit input: the metric would otherwise credit the system
	// for friction that produced nothing (PRD 22.1).
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='IGNORE' WHERE household_id=$1 AND review_type='UNKNOWN_MERCHANT'`, householdID); err != nil {
		t.Fatal(err)
	}
	aggregate, err = NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ExplicitInputs != 0 || aggregate.TypedFields != 0 {
		t.Fatalf("an IGNORE must not count as an explicit input or typed field: %+v", aggregate)
	}
	if len(aggregate.Coverage) != 3 {
		t.Fatalf("every signal that cannot be reconstructed must stay named: %+v", aggregate.Coverage)
	}

	// A resolved typed-field resolution bound to an in-window transaction is one
	// explicit input, one typed field, and one canonical event.
	bankTx := seedTx()
	if _, err := pool.Exec(ctx, `UPDATE review_item SET transaction_id=$2,status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',resolution_values=jsonb_build_object('amount_idr','54000','transaction_at',now()) WHERE household_id=$1 AND review_type='AMBIGUOUS_CATEGORY'`, householdID, bankTx); err != nil {
		t.Fatal(err)
	}
	aggregate, err = NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ExplicitInputs != 1 || aggregate.TypedFields != 1 || aggregate.CanonicalEvents != 1 {
		t.Fatalf("a typed resolution bound to an event is one input, one typed field, one event: %+v", aggregate)
	}
	if aggregate.RHICE != 1 {
		t.Fatalf("one explicit input over one canonical event is RHICE 1: %+v", aggregate)
	}
	// A resolution bound to an event outside the window must not sit in the
	// numerator while its event sits outside the denominator.
	var oldTx string
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,confirmed_at,created_at) VALUES($1,'EXPENSE','CONFIRMED',1000,'IDR',now()-interval '60 days','old',now()-interval '60 days',now()-interval '60 days') RETURNING id`, householdID).Scan(&oldTx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,transaction_id,review_type,status,resolution_action,resolved_at,created_at) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','RESOLVED','COMPLETE_BANK_FACTS',now(),now())`, householdID, telegramEventID, oldTx); err != nil {
		t.Fatal(err)
	}
	aggregate, err = NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.CanonicalEvents != 1 {
		t.Fatalf("an out-of-window event must not enter the denominator: %+v", aggregate)
	}
	if aggregate.ExplicitInputs != 1 {
		t.Fatalf("an out-of-window resolution must not enter the numerator: %+v", aggregate)
	}

	// An accepted proposal is an explicit input that carried no typed value.
	tgTx := seedTx()
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,transaction_id,review_type,status,resolution_action,resolved_at,created_at) VALUES($1,$2,$3,'POSSIBLE_DUPLICATE','RESOLVED','CONFIRM_REVIEW',now(),now())`, householdID, telegramEventID, tgTx); err != nil {
		t.Fatal(err)
	}
	aggregate, err = NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.AcceptedWithoutEdit != 1 {
		t.Fatalf("an accepted proposal is reviewable-without-edit: %+v", aggregate)
	}
	if aggregate.ExplicitInputs != 2 || aggregate.TypedFields != 1 {
		t.Fatalf("accept-without-edit is an input but not a typed field: %+v", aggregate)
	}
	// A system resolution (no human answered) must not inflate RHICE: the
	// numerator is an allow-list of explicit user actions, not a deny-list.
	sysTx := seedTx()
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,transaction_id,review_type,status,resolution_action,resolved_at,created_at) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','RESOLVED','EMAIL_RECEIVED_AT_FALLBACK',now(),now())`, householdID, telegramEventID, sysTx); err != nil {
		t.Fatal(err)
	}
	aggregate, err = NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ExplicitInputs != 2 {
		t.Fatalf("a system resolution must not count as an explicit input: %+v", aggregate)
	}
}
