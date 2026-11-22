package operations

import (
	"context"
	"fmt"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

const stuckTestDecision = `{"version":1,"reasonCode":"MANUAL_CORRECTION","allowedActions":["CONFIRM_REVIEW","IGNORE"],"decisionClass":"EVIDENCE_GAP","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["correction_details"]}`

func TestStuckSourceEventsFindsOnlyEventsWithoutAReviewPath(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := testsupport.UniqueStamp()
	var household string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Stuck events %d", stamp)).Scan(&household))
	event := func(name, status string) string {
		t.Helper()
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode(md5($2),'hex'),$3) RETURNING id`, household, fmt.Sprintf("%s-%d", name, stamp), status).Scan(&id))
		return id
	}
	transactionFor := func(source string) string {
		t.Helper()
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',5000,'IDR',now()) RETURNING id`, household).Scan(&id))
		_, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TELEGRAM_TEXT')`, id, source)
		must(err)
		return id
	}
	item := func(column, value, status string) {
		t.Helper()
		resolved := "NULL"
		if status == "RESOLVED" {
			resolved = "now()"
		}
		_, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO review_item(household_id,%s,review_type,status,resolved_at,decision) VALUES($1,$2,'MANUAL_CORRECTION',$3,%s,$4::jsonb)`, column, resolved), household, value, status, stuckTestDecision)
		must(err)
	}

	bare := event("bare", "NEEDS_REVIEW")
	orphanTx := event("orphan-tx", "NEEDS_REVIEW")
	transactionFor(orphanTx)
	resolvedOnly := event("resolved-only", "NEEDS_REVIEW")
	item("source_event_id", resolvedOnly, "RESOLVED")
	direct := event("direct", "NEEDS_REVIEW")
	item("source_event_id", direct, "OPEN")
	viaTx := event("via-tx", "NEEDS_REVIEW")
	item("transaction_id", transactionFor(viaTx), "OPEN")
	viaProposal := event("via-proposal", "NEEDS_REVIEW")
	var proposal string
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,confidence,proposal_status) VALUES($1,$2,'EXPENSE',100,'IDR',now(),1,'NEEDS_REVIEW') RETURNING id`, household, viaProposal).Scan(&proposal))
	item("proposal_id", proposal, "PENDING_SEND")
	event("processed", "PROCESSED")

	primary, member := event("album-primary", "NEEDS_REVIEW"), event("album-member", "NEEDS_REVIEW")
	var document, attachment string
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,decode(md5($2),'hex'),'image/jpeg',1,1,1,$2) RETURNING id`, household, fmt.Sprintf("album-%d", stamp)).Scan(&attachment))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'NEEDS_REVIEW') RETURNING id`, household, primary, attachment).Scan(&document))
	_, err := pool.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,1)`, document, member, attachment)
	must(err)
	item("document_id", document, "OPEN")

	supersededOnly := event("superseded", "NEEDS_REVIEW")
	supersededTx := transactionFor(supersededOnly)
	item("transaction_id", supersededTx, "OPEN")
	_, err = pool.Exec(ctx, `UPDATE transaction_evidence SET metadata_json=jsonb_build_object('link_status','SUPERSEDED') WHERE transaction_id=$1`, supersededTx)
	must(err)

	stuck, err := StuckSourceEvents(ctx, pool, household)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]StuckSourceEvent{}
	for _, e := range stuck {
		got[e.SourceEventID] = e
	}
	if len(got) != 4 || got[supersededOnly].SourceEventID == "" || got[bare].SourceEventID == "" || got[orphanTx].SourceEventID == "" || got[resolvedOnly].SourceEventID == "" {
		t.Fatalf("want exactly bare, orphan-tx, resolved-only and superseded, got %+v", stuck)
	}
	if got[orphanTx].NeedsReviewTransactions != 1 || got[bare].NeedsReviewTransactions != 0 || !got[resolvedOnly].HadSourceReview || got[bare].HadSourceReview {
		t.Fatalf("diagnostic columns wrong: %+v", got)
	}

	item("source_event_id", bare, "OPEN")
	if after, err := StuckSourceEvents(ctx, pool, household); err != nil || len(after) != 3 {
		t.Fatalf("after adding a review item: %d rows, err=%v", len(after), err)
	}
	var other string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Stuck other %d", stamp)).Scan(&other))
	if none, err := StuckSourceEvents(ctx, pool, other); err != nil || len(none) != 0 {
		t.Fatalf("scoped scan leaked rows: %d err=%v", len(none), err)
	}
}
