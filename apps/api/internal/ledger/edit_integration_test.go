package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestEditTransactionDetails(t *testing.T) {
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
	var household, foreign, user, category, otherCategory, foreignCategory, account, inactive, foreignAccount string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Edit own') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Edit foreign') RETURNING id`).Scan(&foreign))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Editor','unused') RETURNING id`, fmt.Sprintf("edit-%d@example.test", testsupport.UniqueStamp())).Scan(&user))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, household).Scan(&category))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Gaji','gaji') RETURNING id`, household).Scan(&otherCategory))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Asing','asing') RETURNING id`, foreign).Scan(&foreignCategory))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'BCA','BANK','FULL_LEDGER') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy,active) VALUES($1,'Lama','BANK','FULL_LEDGER',false) RETURNING id`, household).Scan(&inactive))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Asing','BANK','FULL_LEDGER') RETURNING id`, foreign).Scan(&foreignAccount))
	h := NewHandler(pool)
	principal := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true}
	newTx := func(kind, status string) string {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,voided_at) VALUES($1,$2,$3,5000,now(),'Awal',CASE WHEN $3='CONFIRMED' THEN now() END,CASE WHEN $3='VOIDED' THEN now() END) RETURNING id`, household, kind, status).Scan(&id))
		return id
	}
	stamp := func(id string) string {
		var s string
		must(pool.QueryRow(ctx, `SELECT to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') FROM transaction WHERE id=$1`, id).Scan(&s))
		return s
	}
	patch := func(id, ifMatch, body string, as auth.Principal) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/transactions/"+id, strings.NewReader(body))
		req.SetPathValue("id", id)
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), as))
		rec := httptest.NewRecorder()
		h.EditTransaction(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body["code"]
	}
	audits := func(id string) int {
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='UPDATE_DETAILS'`, id).Scan(&n))
		return n
	}

	t.Run("happy_path_each_field_and_audit_contents", func(t *testing.T) {
		id := newTx("EXPENSE", "CONFIRMED")
		body := fmt.Sprintf(`{"categoryId":%q,"accountId":%q,"description":"  Makan siang ","note":"enak"}`, category, account)
		rec := patch(id, stamp(id), body, principal)
		if rec.Code != 200 {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		var v transactionView
		must(json.Unmarshal(rec.Body.Bytes(), &v))
		if v.CategoryID == nil || *v.CategoryID != category || v.AccountID == nil || *v.AccountID != account || *v.Description != "Makan siang" || *v.Note != "enak" {
			t.Fatalf("not applied: %+v", v)
		}
		var before, after string
		must(pool.QueryRow(ctx, `SELECT before_json::text,after_json::text FROM audit_log WHERE entity_id=$1 AND action='UPDATE_DETAILS'`, id).Scan(&before, &after))
		if strings.Contains(before, "note\":\"enak") || !strings.Contains(after, `"note": "enak"`) || !strings.Contains(before, `"description": "Awal"`) || !strings.Contains(before, `"categoryId": null`) {
			t.Fatalf("audit before=%s after=%s", before, after)
		}
		rec = patch(id, stamp(id), `{"categoryId":null,"note":null,"description":"Makan siang"}`, principal)
		if rec.Code != 200 {
			t.Fatalf("clear: %d %s", rec.Code, rec.Body)
		}
		must(pool.QueryRow(ctx, `SELECT before_json::text,after_json::text FROM audit_log WHERE entity_id=$1 AND action='UPDATE_DETAILS' ORDER BY created_at DESC,id DESC LIMIT 1`, id).Scan(&before, &after))
		if strings.Contains(after, "description") || strings.Contains(after, "accountId") || !strings.Contains(after, `"categoryId": null`) {
			t.Fatalf("second audit after=%s", after)
		}
	})
	t.Run("noop_writes_nothing", func(t *testing.T) {
		id := newTx("EXPENSE", "CONFIRMED")
		first := stamp(id)
		rec := patch(id, first, `{"description":"Awal"}`, principal)
		if rec.Code != 200 || audits(id) != 0 || stamp(id) != first {
			t.Fatalf("noop: %d audits=%d", rec.Code, audits(id))
		}
	})
	t.Run("cross_household_404", func(t *testing.T) {
		id := newTx("EXPENSE", "CONFIRMED")
		other := auth.Principal{UserID: user, HouseholdID: foreign, HasHousehold: true}
		if rec := patch(id, stamp(id), `{"description":"x"}`, other); rec.Code != 404 {
			t.Fatalf("status %d", rec.Code)
		}
	})
	t.Run("validation_400", func(t *testing.T) {
		id := newTx("EXPENSE", "CONFIRMED")
		for name, tc := range map[string]struct{ body, code string }{
			"foreign_category": {fmt.Sprintf(`{"categoryId":%q}`, foreignCategory), codeInvalidCategory},
			"foreign_account":  {fmt.Sprintf(`{"accountId":%q}`, foreignAccount), codeInvalidAccount},
			"inactive_account": {fmt.Sprintf(`{"accountId":%q}`, inactive), codeInvalidAccount},
			"locked_amount":    {`{"amount":"1"}`, codeLockedField},
			"unknown":          {`{"colour":"red"}`, codeUnknownField},
			"empty":            {`{}`, codeEmptyEdit},
			"blank_desc":       {`{"description":"  "}`, codeInvalidField},
			"bad_uuid":         {`{"categoryId":"nope"}`, codeInvalidField},
		} {
			rec := patch(id, stamp(id), tc.body, principal)
			if rec.Code != 400 || code(rec) != tc.code {
				t.Errorf("%s: status %d code %s", name, rec.Code, code(rec))
			}
		}
		if audits(id) != 0 {
			t.Fatal("rejected edits must not audit")
		}
	})
	t.Run("transfer_locks_category_and_account", func(t *testing.T) {
		id := newTx("TRANSFER", "CONFIRMED")
		if rec := patch(id, stamp(id), fmt.Sprintf(`{"accountId":%q}`, account), principal); rec.Code != 409 || code(rec) != codeTransferLocked {
			t.Fatalf("status %d %s", rec.Code, code(rec))
		}
		if rec := patch(id, stamp(id), `{"note":"ok"}`, principal); rec.Code != 200 {
			t.Fatalf("note on transfer: %d", rec.Code)
		}
	})
	t.Run("voided_409", func(t *testing.T) {
		id := newTx("EXPENSE", "VOIDED")
		if rec := patch(id, stamp(id), `{"description":"x"}`, principal); rec.Code != 409 || code(rec) != codeVoided {
			t.Fatalf("status %d %s", rec.Code, code(rec))
		}
	})
	t.Run("stale_and_missing_if_match", func(t *testing.T) {
		id := newTx("INCOME", "CONFIRMED")
		old := stamp(id)
		if rec := patch(id, old, fmt.Sprintf(`{"categoryId":%q}`, otherCategory), principal); rec.Code != 200 {
			t.Fatalf("first edit %d", rec.Code)
		}
		if rec := patch(id, old, `{"description":"lagi"}`, principal); rec.Code != 409 || code(rec) != codeStale {
			t.Fatalf("stale: %d %s", rec.Code, code(rec))
		}
		if rec := patch(id, "", `{"description":"lagi"}`, principal); rec.Code != 428 {
			t.Fatalf("missing If-Match: %d", rec.Code)
		}
		if audits(id) != 1 {
			t.Fatalf("audits %d", audits(id))
		}
	})
	t.Run("open_review_409", func(t *testing.T) {
		for _, status := range []string{"NEEDS_REVIEW", "CONFIRMED"} {
			id := newTx("EXPENSE", status)
			must(execOnly(pool, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSACTION_FACTS_MISSING','OPEN','{"reasonCode":"TRANSACTION_FACTS_MISSING","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}')`, household, id))
			if rec := patch(id, stamp(id), fmt.Sprintf(`{"categoryId":%q}`, category), principal); rec.Code != 409 || code(rec) != codeReviewOpen {
				t.Fatalf("%s: %d %s", status, rec.Code, code(rec))
			}
		}
	})
	t.Run("category_only_edit_on_null_description", func(t *testing.T) {
		id := newTx("INCOME", "CONFIRMED")
		must(execOnly(pool, `UPDATE transaction SET description=NULL,updated_at=now() WHERE id=$1`, id))
		if rec := patch(id, stamp(id), fmt.Sprintf(`{"categoryId":%q}`, category), principal); rec.Code != 200 {
			t.Fatalf("category-only on NULL description: %d %s", rec.Code, rec.Body)
		}
	})
	t.Run("inactive_current_values_stay_editable_and_account_change_opens_no_review", func(t *testing.T) {
		id := newTx("EXPENSE", "CONFIRMED")
		var staleCat, staleAcct string
		must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Lama','lama') RETURNING id`, household).Scan(&staleCat))
		must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Tutup','BANK','FULL_LEDGER') RETURNING id`, household).Scan(&staleAcct))
		must(execOnly(pool, `UPDATE transaction SET category_id=$2,account_id=$3,updated_at=now() WHERE id=$1`, id, staleCat, staleAcct))
		must(execOnly(pool, `UPDATE category SET active=false WHERE id=$1`, staleCat))
		must(execOnly(pool, `UPDATE account SET active=false WHERE id=$1`, staleAcct))
		if rec := patch(id, stamp(id), `{"description":"Baru"}`, principal); rec.Code != 200 {
			t.Fatalf("description-only: %d %s", rec.Code, rec.Body)
		}
		if rec := patch(id, stamp(id), fmt.Sprintf(`{"categoryId":%q,"accountId":%q,"note":"x"}`, staleCat, staleAcct), principal); rec.Code != 200 {
			t.Fatalf("unchanged inactive: %d %s", rec.Code, rec.Body)
		}
		if rec := patch(id, stamp(id), fmt.Sprintf(`{"accountId":%q}`, inactive), principal); rec.Code != 400 || code(rec) != codeInvalidAccount {
			t.Fatalf("to inactive: %d %s", rec.Code, code(rec))
		}
		var before, after int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1`, household).Scan(&before))
		if rec := patch(id, stamp(id), fmt.Sprintf(`{"accountId":%q}`, account), principal); rec.Code != 200 {
			t.Fatalf("account change: %d %s", rec.Code, rec.Body)
		}
		must(pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1`, household).Scan(&after))
		if before != after {
			t.Fatalf("account change created review items: %d -> %d", before, after)
		}
	})
}

func execOnly(pool *pgxpool.Pool, sql string, args ...any) error {
	_, err := pool.Exec(context.Background(), sql, args...)
	return err
}
