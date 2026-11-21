package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// "Urungkan" on Perlu dicek: a plain category confirmation is undone through the review item's
// own id, and only where reviewdomain.ConfirmedReviewReopenable allows it.
func TestReopenConfirmedCategoryReview(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
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
	household, user, category := seedTransferReviewOwner(t, pool, time.Now().UnixNano())
	principal := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
	handler := NewHandler(pool)
	call := func(method, path, id, body string, fn http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.SetPathValue("id", id)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		fn(w, r)
		return w
	}
	openReview := func(kind string) (transaction string) {
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,purpose) VALUES($1,$2,'NEEDS_REVIEW',25000,now(),'Warung',CASE WHEN $2='TRANSFER' THEN 'INTERNAL_TRANSFER' ELSE 'GENERAL' END) RETURNING id`, household, kind).Scan(&transaction))
		_, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","knownFacts":{},"missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}')`, household, transaction)
		must(err)
		return transaction
	}
	reviewItemID := func(transaction string) string {
		w := call(http.MethodGet, "/api/v1/reviews", "", "", handler.List)
		if w.Code != http.StatusOK {
			t.Fatalf("list %d %s", w.Code, w.Body.String())
		}
		var items []struct {
			ID           string `json:"id"`
			ReviewItemID string `json:"reviewItemId"`
		}
		must(json.Unmarshal(w.Body.Bytes(), &items))
		for _, item := range items {
			if item.ID == transaction {
				return item.ReviewItemID
			}
		}
		return ""
	}

	t.Run("confirm_then_undo_restores_an_open_category_question", func(t *testing.T) {
		transaction := openReview("EXPENSE")
		original := reviewItemID(transaction)
		if original == "" {
			t.Fatal("the list must expose the review item id")
		}
		if w := call(http.MethodPost, "/confirm", transaction, fmt.Sprintf(`{"categoryId":%q}`, category), handler.Confirm); w.Code != http.StatusNoContent {
			t.Fatalf("confirm %d %s", w.Code, w.Body.String())
		}
		if reviewItemID(transaction) != "" {
			t.Fatal("a confirmed transaction must leave the open list")
		}
		resolved := call(http.MethodGet, "/api/v1/reviews/resolved", "", "", handler.Resolved)
		var history []struct {
			ID         string `json:"id"`
			Reopenable bool   `json:"reopenable"`
		}
		must(json.Unmarshal(resolved.Body.Bytes(), &history))
		found := false
		for _, entry := range history {
			if entry.ID == original {
				found = entry.Reopenable
			}
		}
		if !found {
			t.Fatalf("a confirmed category review must be reopenable in history: %+v", history)
		}
		w := call(http.MethodPost, "/reopen", original, "", handler.Reopen)
		if w.Code != http.StatusOK {
			t.Fatalf("reopen %d %s", w.Code, w.Body.String())
		}
		var status string
		var categoryID *string
		var confirmedAt *time.Time
		must(pool.QueryRow(ctx, `SELECT status,category_id::text,confirmed_at FROM transaction WHERE id=$1`, transaction).Scan(&status, &categoryID, &confirmedAt))
		if status != "NEEDS_REVIEW" || categoryID != nil || confirmedAt != nil {
			t.Fatalf("status=%s category=%v confirmedAt=%v", status, categoryID, confirmedAt)
		}
		var open, resolvedCount int
		must(pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status IN ('OPEN','PENDING_SEND')),count(*) FILTER (WHERE status='RESOLVED') FROM review_item WHERE transaction_id=$1`, transaction).Scan(&open, &resolvedCount))
		if open != 1 || resolvedCount != 1 {
			t.Fatalf("open=%d resolved=%d: history must stay and one fresh item must open", open, resolvedCount)
		}
		if reopened := reviewItemID(transaction); reopened == "" || reopened == original {
			t.Fatalf("the list must show the fresh item again, got %q", reopened)
		}
		// A second undo of the same (now superseded) item is refused.
		if w := call(http.MethodPost, "/reopen", original, "", handler.Reopen); w.Code != http.StatusConflict {
			t.Fatalf("second reopen %d", w.Code)
		}
	})

	t.Run("a_transfer_is_not_reopenable", func(t *testing.T) {
		transaction := openReview("TRANSFER")
		item := reviewItemID(transaction)
		_, err := pool.Exec(ctx, `UPDATE transaction SET status='CONFIRMED',confirmed_at=now() WHERE id=$1`, transaction)
		must(err)
		_, err = pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolution_action='CONFIRM_REVIEW',resolved_at=now() WHERE id=$1`, item)
		must(err)
		if w := call(http.MethodPost, "/reopen", item, "", handler.Reopen); w.Code != http.StatusConflict {
			t.Fatalf("transfer reopen %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("a_merge_target_is_not_reopenable", func(t *testing.T) {
		target := openReview("EXPENSE")
		item := reviewItemID(target)
		var source string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,voided_at) VALUES($1,'EXPENSE','VOIDED',25000,now(),now()) RETURNING id`, household).Scan(&source))
		_, err := pool.Exec(ctx, `UPDATE transaction SET status='CONFIRMED',confirmed_at=now(),category_id=$2 WHERE id=$1`, target, category)
		must(err)
		_, err = pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolution_action='CONFIRM_REVIEW',resolved_at=now() WHERE id=$1`, item)
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO reconciliation_merge(household_id,source_transaction_id,target_transaction_id,status,created_by_user_id) VALUES($1,$2,$3,'ACTIVE',$4)`, household, source, target, user)
		must(err)
		if w := call(http.MethodPost, "/reopen", item, "", handler.Reopen); w.Code != http.StatusConflict {
			t.Fatalf("merge target reopen %d %s", w.Code, w.Body.String())
		}
		var status string
		must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, target).Scan(&status))
		if status != "CONFIRMED" {
			t.Fatalf("a refused reopen changed the transaction to %s", status)
		}
	})
}
