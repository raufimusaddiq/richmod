package reviewdomain

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Resolving a review item closes its request, and closing a request resolves
// its conversation, whichever resolver did the work (migration 00098). An
// expired request is closed too; an expired request alone leaves the
// conversation awaiting, because an expired card can be reopened.
func TestReviewItemResolutionCascadesToRequestAndConversation(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f := seedMergedScreenshot(t, ctx, pool, "review-cascade")
	newReview := func(requestStatus string) (item, request string) {
		must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision)
			VALUES($1,$2,'DOCUMENT_CLASSIFICATION','OPEN',$3::jsonb) RETURNING id::text`,
			f.household, f.source, `{"version":1,"reasonCode":"LOW_CONFIDENCE_EXTRACTION","allowedActions":["IGNORE"]}`).Scan(&item))
		must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'DOCUMENT_CLASSIFICATION',$3) RETURNING id::text`, item, f.household, requestStatus).Scan(&request))
		_, err := pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, request)
		must(err)
		return
	}
	state := func(request string) (requestStatus, conversation string) {
		must(pool.QueryRow(ctx, `SELECT r.status,c.state FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id WHERE r.id=$1`, request).Scan(&requestStatus, &conversation))
		return
	}
	for _, from := range []string{"OPEN", "EXPIRED"} {
		item, request := newReview(from)
		if from == "EXPIRED" {
			if status, conversation := state(request); status != "EXPIRED" || conversation != "AWAITING_DETAIL" {
				t.Fatalf("expired request alone: %s/%s, want EXPIRED/AWAITING_DETAIL", status, conversation)
			}
		}
		_, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now() WHERE id=$1`, item)
		must(err)
		if status, conversation := state(request); status != "RESOLVED" || conversation != "RESOLVED" {
			t.Fatalf("%s request after the item was resolved: %s/%s, want RESOLVED/RESOLVED", from, status, conversation)
		}
	}
	// The merchant-learning question is asked after the request is RESOLVED and
	// must survive both the trigger and the repair.
	learnItem, learnRequest := newReview("OPEN")
	_, err = pool.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_MERCHANT_DECISION' WHERE review_request_id=$1`, learnRequest)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now() WHERE id=$1`, learnItem)
	must(err)
	if status, conversation := state(learnRequest); status != "RESOLVED" || conversation != "AWAITING_MERCHANT_DECISION" {
		t.Fatalf("merchant-learning question: %s/%s, want RESOLVED/AWAITING_MERCHANT_DECISION", status, conversation)
	}
	_, request := newReview("OPEN")
	_, err = pool.Exec(ctx, `UPDATE review_request SET status='CANCELLED' WHERE id=$1`, request)
	must(err)
	if status, conversation := state(request); status != "CANCELLED" || conversation != "RESOLVED" {
		t.Fatalf("cancelled request: %s/%s, want CANCELLED/RESOLVED", status, conversation)
	}
}
