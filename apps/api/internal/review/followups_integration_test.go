package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// X-Decision-Count counts a transaction-bound review with the same rule the
// card uses to offer its actions: a review whose only executable answer is
// acknowledging it renders as a notice and is not counted, so the number the
// Inbox shows is exactly the number of decision cards it lists.
func TestTransactionDecisionCountMatchesTheOfferedCards(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for index, decision := range []string{
		// Asks the household: a decision.
		`{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","knownFacts":{},"missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}`,
		// Document reprocessing cannot execute on a transaction binding, so only
		// IGNORE remains: a notice, even though the stored list is not IGNORE-only.
		`{"version":1,"reasonCode":"DOCUMENT_EXTRACTION_LOW_CONFIDENCE","knownFacts":{},"missingFacts":[],"allowedActions":["REPROCESS_DOCUMENT","IGNORE"]}`,
		// A second decision, so the count is not trivially one.
		`{"version":1,"reasonCode":"UNKNOWN_MERCHANT","knownFacts":{},"missingFacts":["merchant"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}`,
	} {
		_, err := f.pool.Exec(ctx, `WITH t AS (INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',$2,$3) RETURNING id) INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) SELECT $1,id,$4::jsonb->>'reasonCode','OPEN',$4::jsonb FROM t`, f.household, 1000+index, at.Add(time.Duration(index)*time.Minute), decision)
		f.must(err)
	}
	items, header := f.list("")
	decisions := 0
	var notice laneItem
	for _, value := range items {
		if value.Presentation == reviewdomain.PresentationDecision {
			decisions++
		}
		if value.ReviewType == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" {
			notice = value
		}
	}
	if notice.Presentation != reviewdomain.PresentationNotice || !slices.Equal(notice.AllowedActions, []string{"IGNORE"}) {
		t.Fatalf("unexecutable stored actions must render as a notice: %+v", notice)
	}
	if got := header.Get("X-Decision-Count"); got != strconv.Itoa(decisions) || decisions != 2 {
		t.Fatalf("X-Decision-Count=%s, decision cards listed=%d (want 2)", got, decisions)
	}
}

// A transfer case offers a merge only into a candidate the resolver accepts:
// each candidate carries the shared merge verdict, MERGE_EXISTING is offered
// only when one is mergeable, and a merge into a refused candidate is a 400
// that no enabled button can produce.
func TestTransferMergeIsOfferedOnlyWhereItIsAccepted(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	var account, other string
	f.must(f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, f.household).Scan(&account))
	f.must(f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening lain','BANK','FULL_LEDGER') RETURNING id`, f.household).Scan(&other))
	transfer := func(accountID, kind, purpose string) string {
		var id string
		f.must(f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,$2,$3,'CONFIRMED',250000,now()-interval '1 hour',$4,now()) RETURNING id`, f.household, accountID, kind, purpose).Scan(&id))
		return id
	}
	seed := func(candidates ...string) (string, string) {
		source := f.source("TELEGRAM_TEXT", "NEEDS_REVIEW")
		_, err := f.pool.Exec(ctx, `INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,candidate_transaction_ids) VALUES($1,$2,$3,250000,now(),'pindah dana','',$4::uuid[])`, f.household, source, account, candidates)
		f.must(err)
		var item string
		f.must(f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","knownFacts":{},"missingFacts":["transfer_relationship"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}'::jsonb) RETURNING id`, f.household, source).Scan(&item))
		return item, source
	}
	type listed struct {
		ID                 string   `json:"id"`
		AllowedActions     []string `json:"allowedActions"`
		TransferCandidates []struct {
			ID        string `json:"id"`
			Mergeable bool   `json:"mergeable"`
		} `json:"transferCandidates"`
	}
	find := func(id string) listed {
		r := f.principal(httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil))
		w := httptest.NewRecorder()
		NewHandler(f.pool).List(w, r)
		var items []listed
		f.must(json.Unmarshal(w.Body.Bytes(), &items))
		for _, value := range items {
			if value.ID == id {
				return value
			}
		}
		t.Fatalf("review %s not listed", id)
		return listed{}
	}
	elsewhere := transfer(other, "TRANSFER", "INTERNAL_TRANSFER")  // a classified transfer on another account
	unclassified := transfer(account, "UNCLASSIFIED", "GENERAL")   // no purpose to keep, and the case proposes none
	accepted := transfer(account, "TRANSFER", "INTERNAL_TRANSFER") // the one a merge accepts
	item, source := seed(elsewhere, unclassified, accepted)
	value := find(item)
	if !slices.Contains(value.AllowedActions, "MERGE_EXISTING") {
		t.Fatalf("a mergeable candidate must be offered: %v", value.AllowedActions)
	}
	verdicts := map[string]bool{}
	for _, candidate := range value.TransferCandidates {
		verdicts[candidate.ID] = candidate.Mergeable
	}
	if len(verdicts) != 3 || verdicts[elsewhere] || verdicts[unclassified] || !verdicts[accepted] {
		t.Fatalf("merge verdicts=%v", verdicts)
	}
	for _, refused := range []string{elsewhere, unclassified} {
		if w := f.resolve(item, `{"action":"MERGE_EXISTING","values":{"transactionId":"`+refused+`"}}`); w.Code != http.StatusBadRequest {
			t.Fatalf("a candidate marked unmergeable must be refused: %d %s", w.Code, w.Body.String())
		}
	}
	if w := f.resolve(item, `{"action":"MERGE_EXISTING","values":{"transactionId":"`+accepted+`"}}`); w.Code != http.StatusNoContent {
		t.Fatalf("a candidate marked mergeable must merge: %d %s", w.Code, w.Body.String())
	}
	if f.status(source) != "PROCESSED" {
		t.Fatalf("merged source=%s", f.status(source))
	}
	// Only unmergeable candidates: the merge is not offered at all, and the
	// dispatcher refuses it with the same rule.
	item, _ = seed(elsewhere, unclassified)
	value = find(item)
	if slices.Contains(value.AllowedActions, "MERGE_EXISTING") || !slices.Contains(value.AllowedActions, "CONFIRM_NEW_TRANSFER") {
		t.Fatalf("no mergeable candidate means no merge offer: %v", value.AllowedActions)
	}
	if w := f.resolve(item, `{"action":"MERGE_EXISTING","values":{"transactionId":"`+elsewhere+`"}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("an unoffered merge must be refused: %d %s", w.Code, w.Body.String())
	}
}
