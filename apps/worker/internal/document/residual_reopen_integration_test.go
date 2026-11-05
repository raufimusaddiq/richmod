package document

import (
	"context"
	"testing"
	"time"
)

// seedWaitingCycle creates a closed cycle whose residual review the household
// answered "transaksi belum tercatat", covering the receipt time.
func seedWaitingCycle(t *testing.T, fixture receiptFixture) string {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var user, salarySource, salaryEventSource, start, end, caseID string
	must(fixture.pool.QueryRow(ctx, `SELECT user_id::text FROM telegram_identity WHERE household_id=$1 LIMIT 1`, fixture.householdID).Scan(&user))
	must(fixture.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',gen_random_uuid()::text,now(),decode(md5(random()::text),'hex'),'PROCESSED') RETURNING id::text`, fixture.householdID).Scan(&salaryEventSource))
	must(fixture.pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id::text`, fixture.householdID, user).Scan(&salarySource))
	for _, row := range []struct {
		date string
		into *string
	}{{"2026-09-01", &start}, {"2026-10-01", &end}} {
		var transaction string
		must(fixture.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id::text`, fixture.householdID, row.date).Scan(&transaction))
		must(fixture.pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1000000,$4,'CONFIRMED',$5) RETURNING id::text`, salarySource, fixture.householdID, row.date, transaction, salaryEventSource).Scan(row.into))
	}
	must(fixture.pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-09-01','2026-10-01',1000000,0,0,1000000) RETURNING id::text`, fixture.householdID, start, end).Scan(&caseID))
	_, err := fixture.pool.Exec(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision,resolved_at,resolution_action) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','RESOLVED','{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","knownFacts":{},"missingFacts":["residual_allocation"],"allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"]}'::jsonb,now(),'TRANSACTION_MISSING')`, fixture.householdID, caseID)
	must(err)
	return caseID
}

// An auto-confirmed receipt is a recorded transaction like any other: a cycle
// waiting on a missing transaction reopens with the recomputed residual.
func TestAutoConfirmedReceiptReopensAWaitingResidual(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt reopens residual")
	caseID := seedWaitingCycle(t, fixture)
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .95}
	at := time.Date(2026, 9, 23, 13, 45, 0, 0, time.FixedZone("WIB", 7*3600))
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", receiptValidation{TransactionAt: at, DateKnown: true}, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var residual string
	if err := fixture.pool.QueryRow(ctx, `SELECT decision->'knownFacts'->>'residual_idr' FROM review_item WHERE cycle_residual_case_id=$1 AND status='OPEN'`, caseID).Scan(&residual); err != nil {
		t.Fatalf("waiting residual did not reopen: %v", err)
	}
	if residual != "942500" {
		t.Fatalf("residual=%s", residual)
	}
}
