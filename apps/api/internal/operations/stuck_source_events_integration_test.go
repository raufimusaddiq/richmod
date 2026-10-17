package operations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const stuckTestDecision = `{"version":1,"reasonCode":"MANUAL_CORRECTION","allowedActions":["CONFIRM_REVIEW","IGNORE"],"decisionClass":"EVIDENCE_GAP","whyNotAutoConfirm":"x","knownFacts":{},"missingFacts":["correction_details"]}`

func TestStuckSourceEventsFindsOnlyEventsWithoutAReviewPath(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
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

	bare := event("bare", "NEEDS_REVIEW")          // stuck: nothing linked at all
	orphanTx := event("orphan-tx", "NEEDS_REVIEW") // stuck: needs-review transaction, no item
	transactionFor(orphanTx)
	resolvedOnly := event("resolved-only", "NEEDS_REVIEW") // stuck: its only item is RESOLVED
	item("source_event_id", resolvedOnly, "RESOLVED")
	direct := event("direct", "NEEDS_REVIEW") // reachable: item on the event
	item("source_event_id", direct, "OPEN")
	viaTx := event("via-tx", "NEEDS_REVIEW") // reachable: item on its transaction
	item("transaction_id", transactionFor(viaTx), "OPEN")
	viaProposal := event("via-proposal", "NEEDS_REVIEW") // reachable: item on its proposal
	var proposal string
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,confidence,proposal_status) VALUES($1,$2,'EXPENSE',100,'IDR',now(),1,'NEEDS_REVIEW') RETURNING id`, household, viaProposal).Scan(&proposal))
	item("proposal_id", proposal, "PENDING_SEND")
	event("processed", "PROCESSED") // not unfinished

	// Album member: only the primary event owns the document; the member is a
	// document_page row and is reachable through the document's review item.
	primary, member := event("album-primary", "NEEDS_REVIEW"), event("album-member", "NEEDS_REVIEW")
	var document, attachment string
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,decode(md5($2),'hex'),'image/jpeg',1,1,1,$2) RETURNING id`, household, fmt.Sprintf("album-%d", stamp)).Scan(&attachment))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'NEEDS_REVIEW') RETURNING id`, household, primary, attachment).Scan(&document))
	_, err := pool.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,1)`, document, member, attachment)
	must(err)
	item("document_id", document, "OPEN")

	// Superseded evidence is not a live link: the event is stuck although a
	// needs-review transaction with an open item is still attached to it.
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

	// Giving the stuck event a review item removes it from the count.
	item("source_event_id", bare, "OPEN")
	if after, err := StuckSourceEvents(ctx, pool, household); err != nil || len(after) != 3 {
		t.Fatalf("after adding a review item: %d rows, err=%v", len(after), err)
	}
	// Another household's stuck events never leak into a scoped scan.
	var other string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Stuck other %d", stamp)).Scan(&other))
	if none, err := StuckSourceEvents(ctx, pool, other); err != nil || len(none) != 0 {
		t.Fatalf("scoped scan leaked rows: %d err=%v", len(none), err)
	}
}

// The runbook quotes the query verbatim so an operator can paste it into psql.
func TestStuckSourceEventsRunbookQuotesTheQuery(t *testing.T) {
	raw, err := os.ReadFile("../../../../docs/runbooks/stuck-source-events.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), strings.ReplaceAll(StuckSourceEventsSQL, "$1::uuid", "NULL::uuid")) {
		t.Fatal("docs/runbooks/stuck-source-events.md no longer quotes StuckSourceEventsSQL (with $1::uuid replaced by NULL::uuid)")
	}
}
