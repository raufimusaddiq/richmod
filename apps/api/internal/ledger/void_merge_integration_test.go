package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/review"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestVoidRefusesActiveMergeTarget(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
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
	var household, user string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Merge void') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Merger','unused') RETURNING id`, fmt.Sprintf("merge-void-%d@example.test", testsupport.UniqueStamp())).Scan(&user))
	principal := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true}
	h, rh := NewHandler(pool), review.NewHandler(pool)
	as := func(r *http.Request) *http.Request {
		return r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
	}

	newTarget := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		h.CreateManualTransaction(w, as(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"type":"EXPENSE","amount":"25000","description":"Kopi"}`))))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %d %s", w.Code, w.Body)
		}
		var body struct{ ID string }
		must(json.Unmarshal(w.Body.Bytes(), &body))
		return body.ID
	}
	newDuplicate := func(status string) (id, item string) {
		t.Helper()
		var src string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',now(),sha256(gen_random_uuid()::text::bytea),'PROCESSED') RETURNING id`, household).Scan(&src))
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE',$2,25000,'IDR',now(),'Kopi dobel',CASE WHEN $2='CONFIRMED' THEN now() END) RETURNING id`, household, status).Scan(&id))
		_, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'BANK_EMAIL','{}')`, id, src)
		must(err)
		if status == "NEEDS_REVIEW" {
			must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN','{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, household, id).Scan(&item))
		}
		return id, item
	}
	mergeInto := func(source, item, target string) string {
		t.Helper()
		tx, err := pool.Begin(ctx)
		must(err)
		defer tx.Rollback(ctx)
		result, err := reviewdomain.MergeDuplicateReview(ctx, tx, reviewdomain.DuplicateCommand{HouseholdID: household, ActorUserID: user, TransactionID: source, TargetTransactionID: target, ReviewItemID: item})
		must(err)
		must(tx.Commit(ctx))
		return result.MergeID
	}
	status := func(id string) string {
		var s string
		must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, id).Scan(&s))
		return s
	}
	void := func(id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/"+id+"/void", nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.VoidTransaction(w, as(r))
		return w
	}
	detail := func(id string) transactionDetail {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+id, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.GetTransaction(w, as(r))
		if w.Code != http.StatusOK {
			t.Fatalf("detail %d %s", w.Code, w.Body)
		}
		var out transactionDetail
		must(json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	unmerge := func(mergeID string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/reconciliation-merges/"+mergeID+"/reverse", nil)
		r.SetPathValue("id", mergeID)
		w := httptest.NewRecorder()
		rh.Unmerge(w, as(r))
		return w.Code
	}
	rawVoid := func(id string) error {
		tx, err := pool.Begin(ctx)
		must(err)
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, `UPDATE transaction SET status='VOIDED',voided_at=now(),updated_at=now() WHERE id=$1`, id)
		return err
	}

	t.Run("web_void_is_refused_while_the_merge_is_active_then_allowed_after_unmerge", func(t *testing.T) {
		target := newTarget()
		source, item := newDuplicate("NEEDS_REVIEW")
		mergeID := mergeInto(source, item, target)
		if status(source) != "VOIDED" || status(target) != "CONFIRMED" {
			t.Fatalf("after merge source=%s target=%s", status(source), status(target))
		}
		onTarget := detail(target).ActiveMerges
		if len(onTarget) != 1 || onTarget[0].ID != mergeID || onTarget[0].Role != "TARGET" || onTarget[0].OtherTransactionID != source || onTarget[0].OtherLabel != "Kopi dobel" || onTarget[0].OtherAmount != "25000.00" && onTarget[0].OtherAmount != "25000" {
			t.Fatalf("target merges: %+v", onTarget)
		}
		onSource := detail(source).ActiveMerges
		if len(onSource) != 1 || onSource[0].Role != "SOURCE" || onSource[0].OtherTransactionID != target || onSource[0].OtherLabel != "Kopi" {
			t.Fatalf("source merges: %+v", onSource)
		}
		w := void(target)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"MERGE_TARGET"`) {
			t.Fatalf("void target: %d %s", w.Code, w.Body)
		}
		var audits int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='VOIDED'`, target).Scan(&audits))
		if status(target) != "CONFIRMED" || audits != 0 {
			t.Fatalf("refused void mutated: %s audits=%d", status(target), audits)
		}
		if err := rawVoid(target); !errors.Is(reviewdomain.MergeTargetError(err), reviewdomain.ErrMergeTargetActive) {
			t.Fatalf("bare void must hit the guard, got %v", err)
		}
		if status(source) != "VOIDED" {
			t.Fatalf("source=%s", status(source))
		}
		if code := unmerge(mergeID); code != http.StatusNoContent {
			t.Fatalf("unmerge=%d", code)
		}
		if status(source) != "NEEDS_REVIEW" || len(detail(target).ActiveMerges) != 0 {
			t.Fatalf("after unmerge source=%s merges=%v", status(source), detail(target).ActiveMerges)
		}
		if w := void(target); w.Code != http.StatusNoContent {
			t.Fatalf("void after unmerge: %d %s", w.Code, w.Body)
		}
	})

	t.Run("review_reject_of_a_review_held_target_is_refused", func(t *testing.T) {
		target, targetItem := newDuplicate("NEEDS_REVIEW")
		source, item := newDuplicate("NEEDS_REVIEW")
		_, err := pool.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{knownFacts}',jsonb_build_object('prior_match_transaction_ids',jsonb_build_array($2::text))) WHERE id=$1`, item, target)
		must(err)
		mergeInto(source, item, target)
		reject := func() error {
			tx, err := pool.Begin(ctx)
			must(err)
			defer tx.Rollback(ctx)
			return reviewdomain.RejectTransactionReview(ctx, tx, reviewdomain.RejectCommand{HouseholdID: household, ActorUserID: user, TransactionID: target, ReviewItemID: targetItem, Action: "REJECT_REVIEW"})
		}
		if err := reject(); !errors.Is(err, reviewdomain.ErrMergeTargetActive) {
			t.Fatalf("reject of merge target: %v", err)
		}
		if status(target) != "NEEDS_REVIEW" {
			t.Fatalf("target=%s", status(target))
		}
	})

	t.Run("chained_merge_voids_the_middle_transaction_and_protects_the_final_target", func(t *testing.T) {
		a, aItem := newDuplicate("NEEDS_REVIEW")
		x, xItem := newDuplicate("NEEDS_REVIEW")
		_, err := pool.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{knownFacts}',jsonb_build_object('prior_match_transaction_ids',jsonb_build_array($2::text))) WHERE id=$1`, xItem, a)
		must(err)
		first := mergeInto(x, xItem, a)
		b := newTarget()
		second := mergeInto(a, aItem, b)
		if status(a) != "VOIDED" || status(b) != "CONFIRMED" {
			t.Fatalf("a=%s b=%s", status(a), status(b))
		}
		if w := void(b); w.Code != http.StatusConflict {
			t.Fatalf("final target must stay protected: %d", w.Code)
		}
		if code := unmerge(second); code != http.StatusNoContent {
			t.Fatalf("unmerge second=%d", code)
		}
		if status(a) != "NEEDS_REVIEW" {
			t.Fatalf("a after unmerge=%s", status(a))
		}
		if code := unmerge(first); code != http.StatusNoContent {
			t.Fatalf("unmerge first=%d", code)
		}
	})
}
