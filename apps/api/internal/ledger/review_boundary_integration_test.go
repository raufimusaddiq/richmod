package ledger

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestLedgerActionsCannotBypassReview(t *testing.T) {
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
	var household, foreignHousehold, user string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Synthetic review boundary') RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Synthetic foreign boundary') RETURNING id`).Scan(&foreignHousehold); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Synthetic actor','unused') RETURNING id`, fmt.Sprintf("boundary-%d@example.test", time.Now().UnixNano())).Scan(&user); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
	h := NewHandler(pool)
	for _, tc := range []struct {
		name, kind, status, review string
		foreign                    bool
	}{
		{"missing_category_date", "EXPENSE", "NEEDS_REVIEW", "OPEN", false},
		{"unclassified", "UNCLASSIFIED", "NEEDS_REVIEW", "PENDING_SEND", false},
		{"missing_item", "INCOME", "NEEDS_REVIEW", "", false},
		{"inconsistent_pending", "EXPENSE", "PENDING", "OPEN", false},
		{"inconsistent_confirmed", "EXPENSE", "CONFIRMED", "OPEN", false},
		{"foreign", "EXPENSE", "NEEDS_REVIEW", "OPEN", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := household
			if tc.foreign {
				owner = foreignHousehold
			}
			var id string
			if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,$2,$3,1000,now(),CASE WHEN $3='CONFIRMED' THEN now() END) RETURNING id`, owner, tc.kind, tc.status).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if tc.review != "" {
				if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSACTION_FACTS_MISSING',$3,'{"reasonCode":"TRANSACTION_FACTS_MISSING","missingFacts":["category","transaction_at"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}')`, owner, id, tc.review); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() string {
				t.Helper()
				var value string
				if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('transaction',to_jsonb(t),'reviews',(SELECT jsonb_agg(to_jsonb(ri)) FROM review_item ri WHERE ri.transaction_id=t.id),'audit',(SELECT count(*) FROM audit_log WHERE entity_id=t.id))::text FROM transaction t WHERE t.id=$1`, id).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			before := snapshot()
			for _, action := range []http.HandlerFunc{h.ConfirmTransaction, h.VoidTransaction} {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/fixture/action", nil)
				r.SetPathValue("id", id)
				r = r.WithContext(auth.ContextWithPrincipal(ctx, principal))
				w := httptest.NewRecorder()
				action(w, r)
				want := http.StatusConflict
				if tc.foreign {
					want = http.StatusNotFound
				}
				if w.Code != want {
					t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
				}
				if snapshot() != before {
					t.Fatal("refused ledger action mutated transaction, review, or audit")
				}
			}
		})
	}
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'INCOME','PENDING',1000,now()) RETURNING id`, household).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, action := range []http.HandlerFunc{h.ConfirmTransaction, h.VoidTransaction} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/fixture/action", nil)
		r.SetPathValue("id", id)
		r = r.WithContext(auth.ContextWithPrincipal(ctx, principal))
		w := httptest.NewRecorder()
		action(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("non-review correction status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
