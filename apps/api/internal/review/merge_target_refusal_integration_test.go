package review

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestWebReviewRoutesRefuseActiveMergeTarget(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	var household, user, target, item, duplicate string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("web merge target %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("web-merge-target-%d@example.test", stamp)).Scan(&user))
	principal := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true}
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name) VALUES($1,'EXPENSE','NEEDS_REVIEW',75000,now(),'Toko') RETURNING id`, household).Scan(&target))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN','{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, household, target).Scan(&item))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name,voided_at) VALUES($1,'EXPENSE','VOIDED',75000,now(),'Toko',now()) RETURNING id`, household).Scan(&duplicate))
	_, err = pool.Exec(ctx, `INSERT INTO reconciliation_merge(household_id,source_transaction_id,target_transaction_id,status,created_by_user_id) VALUES($1,$2,$3,'ACTIVE',$4)`, household, duplicate, target, user)
	must(err)
	h := NewHandler(pool)
	call := func(path, id, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.SetPathValue("id", id)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	snapshot := func() string {
		var s string
		must(pool.QueryRow(ctx, `SELECT t.status||'/'||ri.status||'/'||(SELECT count(*) FROM audit_log WHERE entity_id=t.id)::text FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE t.id=$1`, target).Scan(&s))
		return s
	}
	before := snapshot()
	for name, w := range map[string]*httptest.ResponseRecorder{
		"resolve IGNORE":     call("/api/v1/reviews/"+item+"/resolve", item, `{"action":"IGNORE"}`, h.Resolve),
		"reject transaction": call("/api/v1/reviews/"+target+"/reject", target, ``, h.Reject),
	} {
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"MERGE_TARGET"`) {
			t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body)
		}
		if after := snapshot(); after != before {
			t.Fatalf("%s mutated: %s -> %s", name, before, after)
		}
	}
}
