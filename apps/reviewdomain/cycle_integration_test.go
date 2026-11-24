package reviewdomain

import (
	"errors"
	"testing"
)

func TestApplyCycleResidualRejectsASecondAllocation(t *testing.T) {
	t.Parallel()
	f := newLifecycleFixture(t)
	source := f.source("TELEGRAM_TEXT")
	salarySource := f.id(`INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, f.household, f.user)
	event := func(date string) string {
		transactionID := f.id(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, f.household, date)
		return f.id(`INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1000000,$4,'CONFIRMED',$5) RETURNING id`, salarySource, f.household, date, transactionID, source)
	}
	start, end := event("2026-01-01"), event("2026-02-01")
	caseID := f.id(`INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-01-01','2026-02-01',1000000,0,0,1000000) RETURNING id`, f.household, start, end)
	itemID := f.id(`INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','OPEN','{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"]}'::jsonb) RETURNING id`, f.household, caseID)
	first := f.id(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Tambahan','ASSET','BANK','SAVINGS') RETURNING id`, f.household)
	second := f.id(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Cadangan','ASSET','BANK','SAVINGS') RETURNING id`, f.household)
	allocate := func(account string) error {
		tx, err := f.pool.Begin(f.ctx)
		f.must(err)
		defer tx.Rollback(f.ctx)
		_, err = ApplyCycleResidual(f.ctx, tx, CycleResidualCommand{
			HouseholdID: f.household, CaseID: caseID, ReviewItemID: itemID, ActorUserID: f.user, Action: "ALLOCATE_RETAINED_BALANCE",
			Allocations: []CycleAllocation{{WealthAccountID: account, AmountIDR: "1000000"}},
		})
		if err != nil {
			return err
		}
		return tx.Commit(f.ctx)
	}
	f.must(allocate(first))
	if err := allocate(second); !errors.Is(err, ErrCycleCaseNotFound) {
		t.Fatalf("second allocation error = %v, want ErrCycleCaseNotFound", err)
	}
	var allocations int
	f.must(f.pool.QueryRow(f.ctx, `SELECT count(*) FROM cycle_residual_allocation WHERE cycle_residual_case_id=$1`, caseID).Scan(&allocations))
	if allocations != 1 {
		t.Fatalf("allocations = %d, want 1", allocations)
	}
}
