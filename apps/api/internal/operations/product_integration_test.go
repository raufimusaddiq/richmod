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
	var householdID, eventID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Product Aggregate %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("product-%d", stamp)).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN')`, householdID, eventID); err != nil {
		t.Fatal(err)
	}
	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Events != 1 || aggregate.Confirmed != 0 || aggregate.ReviewedEvents != 1 || aggregate.HumanTouchRate != 1 {
		t.Fatalf("unexpected product aggregate: %+v", aggregate)
	}
	if aggregate.BySource["BANK_EMAIL"] != 1 || aggregate.ReviewBySource["BANK_EMAIL"] != 1 || aggregate.ReviewByReason["AMBIGUOUS_CATEGORY"] != 1 {
		t.Fatalf("source/reason counts missing: %+v", aggregate)
	}
	if len(aggregate.Coverage) == 0 {
		t.Fatal("unmeasurable PRD signals must be explicit")
	}
}
