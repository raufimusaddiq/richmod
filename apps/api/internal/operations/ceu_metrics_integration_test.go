package operations

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Binding outcomes are counted per household over the 30-day window, by
// bounded action name only, and never leak across households.
func TestCEUBindingOutcomesAreCountedPerHouseholdInTheWindow(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var household, other, quiet string
	for index, target := range []*string{&household, &other, &quiet} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("CEU metrics %d-%d", stamp, index)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for action, count := range map[string]int{"EXACT_REPLY_BINDING": 3, "RECENT_CONTEXT_BINDING": 2, "AMBIGUOUS_CONTEXT": 1, "REFERENCE_EXPIRED": 1} {
		for i := 0; i < count; i++ {
			if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action) VALUES($1,'CEU_BINDING',$2)`, household, action); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Outside the 30-day window, another household, and another event type must
	// not be counted.
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action,occurred_at) VALUES($1,'CEU_BINDING','EXACT_REPLY_BINDING',now()-interval '31 days')`, household); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action) VALUES($1,'CEU_BINDING','EXACT_REPLY_BINDING')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action) VALUES($1,'REVIEW_TURN','CONFIRM')`, household); err != nil {
		t.Fatal(err)
	}

	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, household)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"EXACT_REPLY_BINDING": 3, "RECENT_CONTEXT_BINDING": 2, "AMBIGUOUS_CONTEXT": 1, "REFERENCE_EXPIRED": 1}
	if len(aggregate.CEUBinding) != len(want) {
		t.Fatalf("ceuBinding = %v, want %v", aggregate.CEUBinding, want)
	}
	for action, count := range want {
		if aggregate.CEUBinding[action] != count {
			t.Fatalf("%s = %d, want %d (all: %v)", action, aggregate.CEUBinding[action], count, aggregate.CEUBinding)
		}
	}

	// Another household sees only its own single count.
	theirs, err := NewHandler(pool).loadProductAggregate(ctx, other)
	if err != nil || len(theirs.CEUBinding) != 1 || theirs.CEUBinding["EXACT_REPLY_BINDING"] != 1 {
		t.Fatalf("other household ceuBinding=%v err=%v", theirs.CEUBinding, err)
	}
	// A household with no CEU traffic reports an empty, non-nil map, so the JSON
	// is {} and not null.
	none, err := NewHandler(pool).loadProductAggregate(ctx, quiet)
	if err != nil || none.CEUBinding == nil || len(none.CEUBinding) != 0 {
		t.Fatalf("quiet household ceuBinding=%v err=%v, want an empty non-nil map", none.CEUBinding, err)
	}
}
