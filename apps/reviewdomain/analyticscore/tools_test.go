package analyticscore

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAnalyticalArgsAreStrictAndReadOnly(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"record_transaction", `{"cycle_start":null}`},
		{"get_cycle_overview", `{}`},
		{"get_cycle_overview", `{"cycle_start":null,"household_id":"other"}`},
		{"get_cycle_overview", `{"cycle_start":null,"category_ref":null}`},
		{"get_cycle_overview", `{"cycle_start":"2026-02-30"}`},
		{"get_cycle_overview", `{"cycle_start":null}{}`},
		{"get_supporting_transactions", `{"cycle_start":null,"category_ref":null}`},
		{"get_category_drivers", `{"cycle_start":null,"category_ref":""}`},
	} {
		if _, err := DecodeArgs(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Fatalf("accepted %s %s", tc.name, tc.raw)
		}
	}
	for _, tool := range Tools() {
		raw := `{"cycle_start":null}`
		if _, ok := tool.Parameters["properties"].(map[string]any)["category_ref"]; ok {
			raw = `{"cycle_start":null,"category_ref":"category.1"}`
		}
		if _, err := DecodeArgs(tool.Name, json.RawMessage(raw)); err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		if tool.Parameters["additionalProperties"] != false {
			t.Fatalf("non-strict %s", tool.Name)
		}
	}
}

func toolFixture() *Session {
	f := Facts{Version: "cycle-review-v1", Period: reviewPeriod{Start: "2026-08-01", State: "CLOSED"}, Cashflow: reviewCashflow{Expense: "1400000", GrossExpense: "1400000"},
		Categories: []reviewCategory{{reviewChange: reviewChange{reviewValue: reviewValue{ID: "canonical-category", Name: "Dining", Amount: "1400000"}, Median: valuePointer("1350000")},
			Merchants:    []reviewChange{{reviewValue: reviewValue{ID: "canonical-merchant", Name: "Fixture cafe", Amount: "1400000"}}},
			Transactions: []reviewTransaction{{ID: "canonical-transaction", Amount: "1400000", Type: "EXPENSE", Merchant: "Fixture cafe"}}}},
		Members: []reviewValue{{ID: "canonical-user", Name: "Member", Amount: "1400000"}}, Destinations: []reviewValue{{ID: "canonical-account", Name: "Savings", Amount: "100"}},
		Wealth: reviewWealth{Current: &reviewSnapshot{ID: "canonical-snapshot", NetWorth: "1000000", accounts: []string{"canonical-account"}}}}
	s := NewSession(nil, "authorized-household", time.Now())
	s.facts[""] = f
	s.facts[f.Period.Start] = f
	return s
}

func TestAnalyticalDependentReadsAndPrivacy(t *testing.T) {
	s := toolFixture()
	ctx := context.Background()
	dependent := json.RawMessage(`{"cycle_start":"2026-08-01","category_ref":"category.1"}`)
	if _, err := s.Read(ctx, "get_supporting_transactions", dependent); err == nil {
		t.Fatal("unissued ref accepted")
	}
	changes, err := s.Read(ctx, "get_cycle_changes", json.RawMessage(`{"cycle_start":null}`))
	if err != nil {
		t.Fatal(err)
	}
	category := changes["categories"].([]map[string]any)[0]
	if *category["median3"].(*string) != "1350000" || category["current"] != "1400000" {
		t.Fatalf("facts changed: %v", category)
	}
	for _, tool := range Tools() {
		args := json.RawMessage(`{"cycle_start":null}`)
		if _, ok := tool.Parameters["properties"].(map[string]any)["category_ref"]; ok {
			args = dependent
		}
		result, err := s.Read(ctx, tool.Name, args)
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"canonical-", "authorized-household", "household_id", "source_payload", "email", "accounts"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("%s leaked %s: %s", tool.Name, forbidden, raw)
			}
		}
	}
	if _, err := s.Read(ctx, "get_category_drivers", json.RawMessage(`{"cycle_start":null,"category_ref":"canonical-category"}`)); err == nil {
		t.Fatal("canonical category ID accepted")
	}
	other := toolFixture()
	if _, err := other.Read(ctx, "get_category_drivers", dependent); err == nil {
		t.Fatal("refs leaked across requests")
	}
}

func TestAnalyticalParallelReads(t *testing.T) {
	s := toolFixture()
	var wg sync.WaitGroup
	for _, tool := range []string{"get_cycle_overview", "get_cycle_changes", "get_savings_reconciliation", "get_cycle_data_quality"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if _, err := s.Read(context.Background(), name, json.RawMessage(`{"cycle_start":null}`)); err != nil {
				t.Error(err)
			}
		}(tool)
	}
	wg.Wait()
}

func TestNativeChangesKeepZeroPrefixDistinctFromMissing(t *testing.T) {
	s := toolFixture()
	f := s.facts[""]
	c := change("1950000", valuePointer("0"), nil)
	c.Name = "Fixture rent"
	f.Categories[0].reviewChange = c
	f.Comparison.Mode = "ELAPSED_DAYS"
	s.facts[""] = f
	result, err := s.Read(context.Background(), "get_cycle_changes", json.RawMessage(`{"cycle_start":null}`))
	if err != nil {
		t.Fatal(err)
	}
	categories := result["categories"].([]map[string]any)
	if *categories[0]["previous"].(*string) != "0" || categories[0]["relative_delta_vs_previous"].(*string) != nil {
		t.Fatalf("native facts=%v", categories)
	}
}
