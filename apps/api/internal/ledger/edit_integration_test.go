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

func TestEditHandEnteredDateAndAmount(t *testing.T) {
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
	var household, foreign, user string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Facts own') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Facts foreign') RETURNING id`).Scan(&foreign))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Editor','unused') RETURNING id`, fmt.Sprintf("facts-%d@example.test", testsupport.UniqueStamp())).Scan(&user))
	h := NewHandler(pool)
	own := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true}
	other := auth.Principal{UserID: user, HouseholdID: foreign, HasHousehold: true}
	newTx := func(kind, sourceType string) string {
		var id, source string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,$2,'CONFIRMED',5000,(date_trunc('day',now() AT TIME ZONE 'Asia/Jakarta') - interval '10 days' + interval '14 hours 35 minutes') AT TIME ZONE 'Asia/Jakarta','Awal',now()) RETURNING id`, household, kind).Scan(&id))
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,$2,now(),$3,'PROCESSED') RETURNING id`, household, sourceType, []byte(id)).Scan(&source))
		_, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TEST')`, id, source)
		must(err)
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
		req.Header.Set("If-Match", ifMatch)
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
	jakartaDay := func(offset int) string {
		var s string
		must(pool.QueryRow(ctx, `SELECT to_char((now() AT TIME ZONE 'Asia/Jakarta')::date + $1::int,'YYYY-MM-DD')`, offset).Scan(&s))
		return s
	}
	facts := func(id string) (amount, day, clock string) {
		must(pool.QueryRow(ctx, `SELECT amount::text,to_char(transaction_at AT TIME ZONE 'Asia/Jakarta','YYYY-MM-DD'),to_char(transaction_at AT TIME ZONE 'Asia/Jakarta','HH24:MI') FROM transaction WHERE id=$1`, id).Scan(&amount, &day, &clock))
		return
	}
	detail := func(id string) (bool, any) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+id, nil)
		req.SetPathValue("id", id)
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), own))
		rec := httptest.NewRecorder()
		h.GetTransaction(rec, req)
		var body struct {
			Editable bool `json:"factsEditable"`
			Reason   any  `json:"factsLockReason"`
		}
		must(json.Unmarshal(rec.Body.Bytes(), &body))
		return body.Editable, body.Reason
	}

	t.Run("hand_entered_amount_and_date_are_corrected_with_audit_and_original_time_of_day", func(t *testing.T) {
		id := newTx("EXPENSE", "WEB_MANUAL")
		if editable, reason := detail(id); !editable || reason != nil {
			t.Fatalf("hand-entered facts editable=%v reason=%v", editable, reason)
		}
		before := stamp(id)
		day := jakartaDay(-3)
		rec := patch(id, before, `{"amount":"12000","transactionDate":"`+day+`"}`, own)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		amount, gotDay, clock := facts(id)
		if amount != "12000" || gotDay != day || clock != "14:35" {
			t.Fatalf("facts = %s %s %s", amount, gotDay, clock)
		}
		var oldAmount, newAmount, actor string
		must(pool.QueryRow(ctx, `SELECT before_json->>'amount',after_json->>'amount',actor_type FROM audit_log WHERE entity_id=$1 AND action='EDIT_TRANSACTION' AND household_id=$2`, id, household).Scan(&oldAmount, &newAmount, &actor))
		if oldAmount != "5000" || newAmount != "12000" || actor != "USER" {
			t.Fatalf("audit = %s %s %s", oldAmount, newAmount, actor)
		}
		if rec := patch(id, before, `{"amount":"13000"}`, own); rec.Code != http.StatusConflict || code(rec) != "STALE_EDIT" {
			t.Fatalf("stale edit = %d %s", rec.Code, rec.Body)
		}
	})
	t.Run("evidence_backed_and_transfer_facts_stay_locked", func(t *testing.T) {
		for name, tx := range map[string]string{"bank email": newTx("EXPENSE", "BANK_EMAIL"), "provider email": newTx("INCOME", "FINANCIAL_EMAIL"), "photo": newTx("EXPENSE", "WEB_IMAGE"), "transfer": newTx("TRANSFER", "WEB_MANUAL")} {
			if editable, reason := detail(tx); editable || reason == nil {
				t.Fatalf("%s: editable=%v reason=%v", name, editable, reason)
			}
			if rec := patch(tx, stamp(tx), `{"amount":"9000"}`, own); rec.Code != http.StatusBadRequest || code(rec) != "LOCKED_FIELD" {
				t.Fatalf("%s amount edit = %d %s", name, rec.Code, rec.Body)
			}
			if rec := patch(tx, stamp(tx), `{"transactionDate":"`+jakartaDay(-1)+`"}`, own); rec.Code != http.StatusBadRequest || code(rec) != "LOCKED_FIELD" {
				t.Fatalf("%s date edit = %d %s", name, rec.Code, rec.Body)
			}
			if amount, _, _ := facts(tx); amount != "5000" {
				t.Fatalf("%s amount changed to %s", name, amount)
			}
		}
	})
	t.Run("other_household_future_date_and_bad_amount_are_refused", func(t *testing.T) {
		id := newTx("EXPENSE", "TELEGRAM_TEXT")
		if rec := patch(id, stamp(id), `{"amount":"9000"}`, other); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign household = %d %s", rec.Code, rec.Body)
		}
		if rec := patch(id, stamp(id), `{"transactionDate":"`+jakartaDay(1)+`"}`, own); rec.Code != http.StatusBadRequest || code(rec) != "DATE_IN_FUTURE" {
			t.Fatalf("future date = %d %s", rec.Code, rec.Body)
		}
		for _, amount := range []string{"0", "-5", "12.5", "1e3", "abc"} {
			if rec := patch(id, stamp(id), `{"amount":"`+amount+`"}`, own); rec.Code != http.StatusBadRequest || code(rec) != "INVALID_AMOUNT" {
				t.Fatalf("amount %q = %d %s", amount, rec.Code, rec.Body)
			}
		}
		if amount, _, _ := facts(id); amount != "5000" {
			t.Fatalf("refused edits changed amount to %s", amount)
		}
	})
}
