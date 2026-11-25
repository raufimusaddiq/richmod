package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestMarkPayslipIncomeAsSalary(t *testing.T) {
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
	stamp := testsupport.UniqueStamp()
	var household, foreign, user string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Mark salary own') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Mark salary foreign') RETURNING id`).Scan(&foreign))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Marker','unused') RETURNING id`, fmt.Sprintf("mark-salary-%d@example.test", stamp)).Scan(&user))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user)
		return err
	}())
	seq := 0
	payslipIncome := func(hh, at string) string {
		t.Helper()
		seq++
		external := fmt.Sprintf("mark-salary-%d-%d", stamp, seq)
		var source, attachment, document, transaction string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED') RETURNING id`, hh, external, []byte(external)).Scan(&source))
		must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, hh, []byte(external), hh+"/"+external+".png").Scan(&attachment))
		must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'PAYSLIP','EXTRACTED') RETURNING id`, hh, source, attachment).Scan(&document))
		_, err := pool.Exec(ctx, `INSERT INTO document_extraction(document_id,stage,schema_version,output_json,confidence,validated) VALUES($1,'PAYSLIP','v1','{"period":"2026-10","employer":"PT Contoh","net_pay":"8500000"}',.96,true)`, document)
		must(err)
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,counterparty_name,confirmed_at) VALUES($1,'INCOME','CONFIRMED',8500000,'IDR',$2,'Penghasilan dari slip gaji','PT Contoh',now()) RETURNING id`, hh, at).Scan(&transaction))
		_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'PAYSLIP_IMAGE',.96,jsonb_build_object('document_id',$3::uuid))`, transaction, source, document)
		must(err)
		return transaction
	}
	h := NewHandler(pool)
	own := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true}
	call := func(id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/"+id+"/mark-salary", nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.MarkSalary(w, r.WithContext(auth.ContextWithPrincipal(r.Context(), own)))
		return w
	}
	canMark := func(id string) bool {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+id, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.GetTransaction(w, r.WithContext(auth.ContextWithPrincipal(r.Context(), own)))
		var body struct {
			CanMarkSalary bool `json:"canMarkSalary"`
		}
		must(json.Unmarshal(w.Body.Bytes(), &body))
		return body.CanMarkSalary
	}

	income := payslipIncome(household, "2026-10-31T20:00:00Z")
	if !canMark(income) {
		t.Fatal("a confirmed payslip income without a salary event must be markable")
	}
	if w := call(income); w.Code != http.StatusOK {
		t.Fatalf("mark salary=%d %s", w.Code, w.Body)
	}
	var primary bool
	var payDate, period, auditedEvent string
	var jobs int
	must(pool.QueryRow(ctx, `SELECT ss.is_primary,se.pay_date::text,to_char(se.payroll_period,'YYYY-MM'),
		(SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'end_salary_event_id'=se.id::text),
		COALESCE((SELECT a.after_json->>'salary_event_id' FROM audit_log a WHERE a.entity_id=se.transaction_id AND a.action='MARK_PAYSLIP_SALARY' AND a.actor_type='USER'),'')
		FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.transaction_id=$1 AND se.status='CONFIRMED'`, income).Scan(&primary, &payDate, &period, &jobs, &auditedEvent))
	if !primary || payDate != "2026-11-01" || period != "2026-10" || jobs != 1 || auditedEvent == "" {
		t.Fatalf("salary primary=%v payDate=%s period=%s jobs=%d audited=%q", primary, payDate, period, jobs, auditedEvent)
	}
	if canMark(income) {
		t.Fatal("a recorded salary must no longer be markable")
	}
	if w := call(income); w.Code != http.StatusConflict {
		t.Fatalf("marking twice=%d %s", w.Code, w.Body)
	}
	if w := call(payslipIncome(household, "2026-10-30T03:00:00Z")); w.Code != http.StatusConflict {
		t.Fatalf("a second payslip for the same source and period=%d %s", w.Code, w.Body)
	}
	var plain string
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,confirmed_at) VALUES($1,'INCOME','CONFIRMED',8500000,'IDR',now(),'PT Contoh',now()) RETURNING id`, household).Scan(&plain))
	if canMark(plain) {
		t.Fatal("an income without payslip evidence must not be markable")
	}
	if w := call(plain); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-payslip income=%d %s", w.Code, w.Body)
	}
	if w := call(payslipIncome(foreign, "2026-10-31T03:00:00Z")); w.Code != http.StatusNotFound {
		t.Fatalf("another household's payslip=%d %s", w.Code, w.Body)
	}
	var events int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM salary_event WHERE household_id IN ($1,$2)`, household, foreign).Scan(&events))
	if events != 1 {
		t.Fatalf("refusals must not record salary, events=%d", events)
	}
}
