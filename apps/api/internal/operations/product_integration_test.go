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
	if len(aggregate.Coverage) != 2 {
		t.Fatalf("signals that cannot be reconstructed must stay named: %+v", aggregate.Coverage)
	}

	// A resolved typed-field resolution is one explicit input and one typed field;
	// with no transaction in the window the denominator stays zero, so RHICE is
	// reported as zero rather than a division artifact.
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',resolution_values=jsonb_build_object('amount_idr','54000','transaction_at',now()) WHERE household_id=$1 AND review_type='AMBIGUOUS_CATEGORY'`, householdID); err != nil {
		t.Fatal(err)
	}
	aggregate, err = NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ExplicitInputs != 1 || aggregate.TypedFields != 1 {
		t.Fatalf("a typed resolution must count as one explicit input and one typed field: %+v", aggregate)
	}
	if aggregate.CanonicalEvents != 0 || aggregate.RHICE != 0 {
		t.Fatalf("RHICE must stay zero without a canonical event to divide by: %+v", aggregate)
	}
	// An accepted proposal is an explicit input that carried no typed value.
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolution_action,resolved_at,created_at) VALUES($1,$2,'POSSIBLE_DUPLICATE','RESOLVED','CONFIRM_REVIEW',now(),now())`, householdID, telegramEventID); err != nil {
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
}
