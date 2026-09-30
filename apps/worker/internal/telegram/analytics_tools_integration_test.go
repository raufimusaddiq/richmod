package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type analyticalTelegramGateway struct {
	responses []gateway.AgentResponse
	requests  []gateway.AgentRequest
}

func (g *analyticalTelegramGateway) AgentTurn(_ context.Context, _ string, req gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.requests = append(g.requests, req)
	i := len(g.requests) - 1
	if i >= len(g.responses) {
		return gateway.AgentResponse{}, fmt.Errorf("unexpected analytical phase")
	}
	return g.responses[i], nil
}

func TestTelegramAnalyticalQuestionsReuseCanonicalEngineAndNaturalReply(t *testing.T) {
	questions := []struct {
		question  string
		reads     []string
		dependent string
	}{
		{"bulan ini paling naik di mana?", []string{"get_cycle_overview", "get_cycle_changes"}, "get_merchant_drivers"},
		{"kenapa expense cycle ini lebih besar?", []string{"get_cycle_overview", "get_cycle_changes"}, "get_supporting_transactions"},
		{"dibanding 3 cycle terakhir gimana?", []string{"get_cycle_overview", "get_cycle_changes"}, "get_category_drivers"},
		{"surplus cycle ini larinya ke mana?", []string{"get_cycle_overview", "get_savings_reconciliation"}, ""},
		{"net worth naik karena cashflow atau valuasi?", []string{"get_cycle_overview", "get_wealth_reconciliation", "get_cycle_data_quality"}, ""},
	}
	for index, tc := range questions {
		t.Run(tc.question, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, fmt.Sprintf("analytics-%d", index))
			var salary, category, merchant string
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Fixture salary','fixture salary',true) RETURNING id`, f.householdID, f.userID).Scan(&salary))
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&category))
			mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Fixture cafe') RETURNING id`, f.householdID).Scan(&merchant))
			for i, date := range []string{"2026-05-01", "2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
				var income string
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',10000000,($2::date::timestamp AT TIME ZONE 'Asia/Jakarta'),now()) RETURNING id`, f.householdID, date).Scan(&income))
				_, err := f.pool.Exec(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,10000000,$4,'CONFIRMED',$5)`, salary, f.householdID, date, income, f.sourceID)
				mustAgentTest(t, err)
				if i < 4 {
					_, err = f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,category_id,merchant_id) VALUES($1,'EXPENSE','CONFIRMED',$2::numeric,($3::date::timestamp AT TIME ZONE 'Asia/Jakarta')+interval '4 days',now(),$4,$5)`, f.householdID, []string{"1350000", "1500000", "700000", "1400000"}[i], date, category, merchant)
					mustAgentTest(t, err)
				}
			}
			state := f.state
			state.ModelPhases = 0
			state.Now = time.Date(2026, 9, 10, 12, 0, 0, 0, jakartaLocation())
			state.Tools = AgentFinanceTools(nil, false, false, false, "", false, false, "")
			state.TurnContext = map[string]any{"user_message": tc.question}
			model := &analyticalTelegramGateway{}
			first := gateway.AgentResponse{ResponseID: "first"}
			for i, name := range tc.reads {
				first.ToolCalls = append(first.ToolCalls, gateway.ToolCall{CallID: fmt.Sprintf("first-%d", i), Name: name, Arguments: json.RawMessage(`{"cycle_start":"2026-08-01"}`)})
			}
			model.responses = append(model.responses, first)
			if tc.dependent != "" {
				model.responses = append(model.responses, gateway.AgentResponse{ResponseID: "dependent", ToolCalls: []gateway.ToolCall{{CallID: "dependent", Name: tc.dependent, Arguments: json.RawMessage(`{"cycle_start":"2026-08-01","category_ref":"category.1"}`)}}})
			}
			message := "Pengeluaran cycle Rp1.400.000. Bandingkan data pendukung sebelum membahasnya."
			model.responses = append(model.responses, gateway.AgentResponse{Text: message})
			p := NewProcessor(f.pool, nil)
			mustAgentTest(t, p.runAgentLoop(ctx, model, state))
			if state.SideEffects != 0 || len(model.requests) != len(model.responses) {
				t.Fatalf("side effects=%d phases=%d", state.SideEffects, len(model.requests))
			}
			facts, err := analyticscore.Load(ctx, f.pool, f.householdID, "2026-08-01", state.Now)
			mustAgentTest(t, err)
			if state.History[0].Facts["facts_version"] != facts.Version {
				t.Fatal("Telegram forked fact engine")
			}
			cash := state.History[0].Facts["cashflow"]
			raw, err := json.Marshal(cash)
			mustAgentTest(t, err)
			canonical, err := json.Marshal(facts.Cashflow)
			mustAgentTest(t, err)
			if string(raw) != string(canonical) {
				t.Fatalf("Telegram cashflow differs: %s vs %s", raw, canonical)
			}
			for i, req := range model.requests {
				for _, tool := range req.Tools {
					if tool.Name == "render_cycle_commentary" {
						t.Fatal("Telegram response protocol changed")
					}
				}
				if i > 0 && len(req.ToolOutputs) == 0 {
					t.Fatal("native dependent continuation missing")
				}
				payload, err := json.Marshal(req)
				mustAgentTest(t, err)
				for _, id := range []string{f.householdID, f.userID, salary, category, merchant} {
					if strings.Contains(string(payload), id) {
						t.Fatal("canonical ID reached Telegram model")
					}
				}
			}
			var reply string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 ORDER BY created_at DESC LIMIT 1`, fmt.Sprint(f.chatID)).Scan(&reply))
			if reply != message {
				t.Fatalf("natural reply altered: %q", reply)
			}
			var count int
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&count))
			if count != 9 {
				t.Fatalf("analytical turn mutated ledger: %d", count)
			}
		})
	}
}
