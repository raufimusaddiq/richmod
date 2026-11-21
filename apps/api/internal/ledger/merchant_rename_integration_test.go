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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// "Tempat · Ubah": PATCH merchantName resolves a household merchant, repoints the raw
// source name's alias, keeps counterparty_name as evidence, and audits both changes.
func TestEditTransactionMerchantRename(t *testing.T) {
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
	var household, foreign, user, category string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Rename own') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Rename foreign') RETURNING id`).Scan(&foreign))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Renamer','unused') RETURNING id`, fmt.Sprintf("rename-%d@example.test", time.Now().UnixNano())).Scan(&user))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Transport','transport') RETURNING id`, household).Scan(&category))
	h := NewHandler(pool)
	principal := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
	newTx := func(owner, kind, status, counterparty string) string {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,counterparty_name,confirmed_at,voided_at,purpose) VALUES($1,$2,$3,5000,now(),'Awal',NULLIF($4,''),CASE WHEN $3='CONFIRMED' THEN now() END,CASE WHEN $3='VOIDED' THEN now() END,CASE WHEN $2='TRANSFER' THEN 'INTERNAL_TRANSFER' ELSE 'GENERAL' END) RETURNING id`, owner, kind, status, counterparty).Scan(&id))
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
	aliasTarget := func(owner, raw string) (name string, ok bool) {
		err := pool.QueryRow(ctx, `SELECT m.normalized_name FROM merchant_alias a JOIN merchant m ON m.id=a.normalized_merchant_id WHERE a.household_id=$1 AND lower(a.raw_name)=lower($2)`, owner, raw).Scan(&name)
		return name, err == nil
	}

	t.Run("creates_merchant_and_alias_and_keeps_raw_counterparty", func(t *testing.T) {
		id := newTx(household, "EXPENSE", "CONFIRMED", "QRIS GRAB*1234")
		rec := patch(id, stamp(id), `{"merchantName":"  Grab   Ride "}`, principal)
		if rec.Code != 200 {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		var v transactionView
		must(json.Unmarshal(rec.Body.Bytes(), &v))
		if v.MerchantName == nil || *v.MerchantName != "Grab Ride" || v.Counterparty == nil || *v.Counterparty != "QRIS GRAB*1234" {
			t.Fatalf("merchant=%v counterparty=%v", v.MerchantName, v.Counterparty)
		}
		if name, ok := aliasTarget(household, "QRIS GRAB*1234"); !ok || name != "Grab Ride" {
			t.Fatalf("alias target=%q ok=%t", name, ok)
		}
		var aliasConfirmed, autoApply bool
		must(pool.QueryRow(ctx, `SELECT created_from_user_confirmation,auto_apply FROM merchant_alias WHERE household_id=$1 AND raw_name='QRIS GRAB*1234'`, household).Scan(&aliasConfirmed, &autoApply))
		if !aliasConfirmed || autoApply {
			t.Fatalf("alias flags confirmed=%t auto=%t", aliasConfirmed, autoApply)
		}
		var before, after string
		must(pool.QueryRow(ctx, `SELECT before_json::text,after_json::text FROM audit_log WHERE entity_id=$1 AND action='UPDATE_DETAILS'`, id).Scan(&before, &after))
		if !strings.Contains(after, `"merchantName": "Grab Ride"`) || !strings.Contains(before, `"merchantName": null`) {
			t.Fatalf("audit before=%s after=%s", before, after)
		}
		var aliasAudit string
		var aliasID string
		must(pool.QueryRow(ctx, `SELECT id::text FROM merchant_alias WHERE household_id=$1 AND raw_name='QRIS GRAB*1234'`, household).Scan(&aliasID))
		must(pool.QueryRow(ctx, `SELECT after_json::text FROM audit_log WHERE entity_id=$1 AND action='UPDATE_MERCHANT_ALIAS' AND entity_type='merchant_alias'`, aliasID).Scan(&aliasAudit))
		if !strings.Contains(aliasAudit, "QRIS GRAB*1234") {
			t.Fatalf("alias audit=%s", aliasAudit)
		}
	})
	t.Run("alias_keeps_existing_category_rule_and_future_lookup_resolves", func(t *testing.T) {
		var old string
		must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'OLD NAME') RETURNING id`, household).Scan(&old))
		_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'GOJEK 99',$2,$3,true,true)`, household, old, category)
		must(err)
		id := newTx(household, "EXPENSE", "CONFIRMED", "gojek  99")
		if rec := patch(id, stamp(id), `{"merchantName":"Gojek"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		var target string
		var def *string
		var auto bool
		must(pool.QueryRow(ctx, `SELECT m.normalized_name,a.default_category_id::text,a.auto_apply FROM merchant_alias a JOIN merchant m ON m.id=a.normalized_merchant_id WHERE a.household_id=$1 AND lower(a.raw_name)='gojek 99'`, household).Scan(&target, &def, &auto))
		if target != "Gojek" || def == nil || *def != category || !auto {
			t.Fatalf("alias clobbered or not repointed: target=%s cat=%v auto=%t", target, def, auto)
		}
		// A later transaction with the same raw name resolves to the clean merchant via the same normalized lookup bank-email uses.
		var resolved string
		must(pool.QueryRow(ctx, `SELECT min(normalized_merchant_id::text) FROM merchant_alias WHERE household_id=$1 AND lower(regexp_replace(btrim(raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g'))`, household, "GOJEK   99").Scan(&resolved))
		var wanted string
		must(pool.QueryRow(ctx, `SELECT id::text FROM merchant WHERE household_id=$1 AND normalized_name='Gojek'`, household).Scan(&wanted))
		if resolved != wanted {
			t.Fatalf("future lookup resolved %s, want %s", resolved, wanted)
		}
	})
	t.Run("reuses_existing_merchant_case_insensitively", func(t *testing.T) {
		id := newTx(household, "EXPENSE", "CONFIRMED", "SHOPEE 1")
		if rec := patch(id, stamp(id), `{"merchantName":"Gojek"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d", rec.Code)
		}
		id2 := newTx(household, "EXPENSE", "CONFIRMED", "SHOPEE 2")
		if rec := patch(id2, stamp(id2), `{"merchantName":"GOJEK"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d", rec.Code)
		}
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE household_id=$1 AND lower(normalized_name)='gojek'`, household).Scan(&n))
		if n != 1 {
			t.Fatalf("merchant rows=%d", n)
		}
	})
	t.Run("case_only_rename_persists_and_is_audited", func(t *testing.T) {
		id := newTx(household, "EXPENSE", "CONFIRMED", "KOPI KENANGAN 7")
		if rec := patch(id, stamp(id), `{"merchantName":"KOPI KENANGAN"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		var merchantRow string
		must(pool.QueryRow(ctx, `SELECT merchant_id::text FROM transaction WHERE id=$1`, id).Scan(&merchantRow))
		if rec := patch(id, stamp(id), `{"merchantName":"Kopi Kenangan"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		var name, sameRow string
		must(pool.QueryRow(ctx, `SELECT m.normalized_name,m.id::text FROM transaction t JOIN merchant m ON m.id=t.merchant_id WHERE t.id=$1`, id).Scan(&name, &sameRow))
		if name != "Kopi Kenangan" || sameRow != merchantRow {
			t.Fatalf("case-only rename name=%q row changed=%t", name, sameRow != merchantRow)
		}
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='UPDATE_DETAILS'`, id).Scan(&n))
		if n != 2 {
			t.Fatalf("expected both renames audited, got %d", n)
		}
		var aliasID string
		must(pool.QueryRow(ctx, `SELECT id::text FROM merchant_alias WHERE household_id=$1 AND raw_name='KOPI KENANGAN 7'`, household).Scan(&aliasID))
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='UPDATE_MERCHANT_ALIAS' AND entity_type='merchant_alias'`, aliasID).Scan(&n))
		if n != 2 {
			t.Fatalf("alias audits keyed by alias id=%d", n)
		}
	})
	t.Run("noop_same_merchant_writes_nothing", func(t *testing.T) {
		id := newTx(household, "EXPENSE", "CONFIRMED", "TOKO A")
		if rec := patch(id, stamp(id), `{"merchantName":"Toko A"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d", rec.Code)
		}
		first := stamp(id)
		if rec := patch(id, first, `{"merchantName":"Toko A"}`, principal); rec.Code != 200 || stamp(id) != first {
			t.Fatalf("noop changed stamp: %d", rec.Code)
		}
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='UPDATE_DETAILS'`, id).Scan(&n))
		if n != 1 {
			t.Fatalf("audits=%d", n)
		}
	})
	t.Run("no_raw_source_name_skips_alias_but_sets_merchant", func(t *testing.T) {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',5000,now(),now()) RETURNING id`, household).Scan(&id))
		if rec := patch(id, stamp(id), `{"merchantName":"Warung"}`, principal); rec.Code != 200 {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='UPDATE_MERCHANT_ALIAS' AND after_json->>'after'=(SELECT id::text FROM merchant WHERE household_id=$1 AND normalized_name='Warung')`, household).Scan(&n))
		if n != 0 {
			t.Fatalf("alias audits=%d", n)
		}
	})
	t.Run("guards", func(t *testing.T) {
		stale := newTx(household, "EXPENSE", "CONFIRMED", "STALE 1")
		if rec := patch(stale, "2000-01-01T00:00:00Z", `{"merchantName":"X"}`, principal); rec.Code != 409 || code(rec) != codeStale {
			t.Fatalf("stale: %d %s", rec.Code, rec.Body)
		}
		if rec := patch(stale, "", `{"merchantName":"X"}`, principal); rec.Code != http.StatusPreconditionRequired {
			t.Fatalf("no if-match: %d", rec.Code)
		}
		review := newTx(household, "EXPENSE", "NEEDS_REVIEW", "REVIEW 1")
		if rec := patch(review, stamp(review), `{"merchantName":"X"}`, principal); rec.Code != 409 || code(rec) != codeReviewOpen {
			t.Fatalf("review: %d %s", rec.Code, rec.Body)
		}
		voided := newTx(household, "EXPENSE", "VOIDED", "VOID 1")
		if rec := patch(voided, stamp(voided), `{"merchantName":"X"}`, principal); rec.Code != 409 || code(rec) != codeVoided {
			t.Fatalf("voided: %d %s", rec.Code, rec.Body)
		}
		transfer := newTx(household, "TRANSFER", "CONFIRMED", "TRF 1")
		if rec := patch(transfer, stamp(transfer), `{"merchantName":"X"}`, principal); rec.Code != 409 || code(rec) != codeTransferLocked {
			t.Fatalf("transfer: %d %s", rec.Code, rec.Body)
		}
		ok := newTx(household, "EXPENSE", "CONFIRMED", "VALID 1")
		for name, body := range map[string]string{"empty": `{"merchantName":"   "}`, "null": `{"merchantName":null}`, "number": `{"merchantName":3}`, "long": `{"merchantName":"` + strings.Repeat("a", 161) + `"}`, "raw_id_still_locked": `{"merchantId":"11111111-1111-4111-8111-111111111111"}`} {
			if rec := patch(ok, stamp(ok), body, principal); rec.Code != 400 {
				t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
			}
		}
		if rec := patch(ok, stamp(ok), `{"merchantId":"11111111-1111-4111-8111-111111111111"}`, principal); code(rec) != codeLockedField {
			t.Fatalf("merchantId must stay locked: %s", rec.Body)
		}
		if _, found := aliasTarget(household, "STALE 1"); found {
			t.Fatal("a refused edit created an alias")
		}
	})
	t.Run("household_isolation", func(t *testing.T) {
		id := newTx(household, "EXPENSE", "CONFIRMED", "ISO 1")
		other := auth.Principal{UserID: user, HouseholdID: foreign, HasHousehold: true}
		if rec := patch(id, stamp(id), `{"merchantName":"Intruder"}`, other); rec.Code != 404 {
			t.Fatalf("status %d", rec.Code)
		}
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE normalized_name='Intruder'`).Scan(&n))
		if n != 0 {
			t.Fatal("foreign caller created a merchant")
		}
		// The same raw name in another household is untouched by this household's alias.
		if _, found := aliasTarget(foreign, "QRIS GRAB*1234"); found {
			t.Fatal("alias leaked across households")
		}
	})
}
