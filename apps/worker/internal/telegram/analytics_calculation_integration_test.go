package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestNativeLegacyCycleDatesRefundsAndCategoryCompleteness(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "analytics-calculation")
	f.state.Now = time.Date(2026, 9, 10, 12, 0, 0, 0, jakartaLocation())
	var salary string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Fixture salary','fixture salary',true) RETURNING id`, f.householdID, f.userID).Scan(&salary))
	for _, date := range []string{"2026-08-01", "2026-09-01"} {
		var income string
		mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',10000000,($2::date::timestamp AT TIME ZONE 'Asia/Jakarta'),now()) RETURNING id`, f.householdID, date).Scan(&income))
		_, err := f.pool.Exec(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,10000000,$4,'CONFIRMED',$5)`, salary, f.householdID, date, income, f.sourceID)
		mustAgentTest(t, err)
	}
	_, err := f.pool.Exec(ctx, `INSERT INTO category(household_id,name,slug) SELECT $1,'Fixture '||n,'fixture-'||n FROM generate_series(1,21) n`, f.householdID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,category_id) SELECT $1,CASE WHEN slug='fixture-21' THEN 'REFUND' ELSE 'EXPENSE' END,'CONFIRMED',CASE WHEN slug='fixture-21' THEN 200 ELSE 100 END,'2026-09-01T00:00:00+07:00',now(),id FROM category WHERE household_id=$1`, f.householdID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',300,'2026-08-31T23:59:59+07:00',now()),($1,'EXPENSE','CONFIRMED',9999999,'2026-09-11T00:00:00+07:00',now()),($1,'EXPENSE','NEEDS_REVIEW',9999999,'2026-09-01T00:00:00+07:00',NULL)`, f.householdID)
	mustAgentTest(t, err)
	// Foreign household data must never affect totals or the truncation count.
	other := newAgentIntegrationFixture(t, "analytics-foreign")
	_, err = f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',9999999,'2026-09-01T00:00:00+07:00',now())`, other.householdID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)
	for _, tc := range []struct{ period, from, to, expense string }{
		{"CURRENT_CYCLE", "2026-09-01T00:00:00+07:00", "2026-09-11T00:00:00+07:00", "1800"},
		{"PREVIOUS_CYCLE", "2026-08-01T00:00:00+07:00", "2026-09-01T00:00:00+07:00", "300"},
	} {
		result, err := p.executeAgentRead(ctx, f.state, gateway.ToolCall{Name: "query_cashflow"}, map[string]any{"period": tc.period}, "fixture")
		mustAgentTest(t, err)
		period := result.Facts["period"].(map[string]any)
		if period["from"] != tc.from || period["to_exclusive"] != tc.to || result.Facts["income_idr"] != "10000000" || result.Facts["expense_idr"] != tc.expense {
			t.Fatalf("%s: %+v", tc.period, result.Facts)
		}
	}
	result, err := p.executeAgentRead(ctx, f.state, gateway.ToolCall{Name: "get_category_breakdown"}, map[string]any{"period": "CURRENT_CYCLE"}, "fixture")
	mustAgentTest(t, err)
	items := result.Facts["categories"].([]map[string]any)
	if len(items) != 20 || items[0]["amount_idr"] != "-200" || result.Facts["total_categories"] != 21 || result.Facts["net_expense_idr"] != "1800" || result.Facts["truncated"] != true {
		t.Fatalf("breakdown=%+v", result.Facts)
	}
}
