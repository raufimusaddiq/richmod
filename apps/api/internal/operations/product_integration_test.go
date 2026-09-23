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
	// Two reviewed events (one bank, one telegram) plus two canonical terminal
	// events. Human-touch rate is distinct reviewed events (2) over canonical
	// events (3); a lingering NEEDS_REVIEW event must not inflate the denominator.
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
	if aggregate.SourceEvents != 4 || aggregate.CanonicalEvents != 2 || aggregate.Confirmed != 1 {
		t.Fatalf("unexpected product aggregate: %+v", aggregate)
	}
	if aggregate.ReviewedEvents != 2 || aggregate.HumanTouchRate != 1 {
		t.Fatalf("human-touch rate must count distinct reviewed events: %+v", aggregate)
	}
	if aggregate.BySource["BANK_EMAIL"] != 2 || aggregate.ReviewBySource["BANK_EMAIL"] != 1 || aggregate.ReviewBySource["TELEGRAM_TEXT"] != 1 || aggregate.ReviewByReason["AMBIGUOUS_CATEGORY"] != 1 || aggregate.ReviewByReason["UNKNOWN_MERCHANT"] != 1 {
		t.Fatalf("source/reason counts missing: %+v", aggregate)
	}
	if len(aggregate.Coverage) == 0 {
		t.Fatal("unmeasurable PRD signals must be explicit")
	}
}
