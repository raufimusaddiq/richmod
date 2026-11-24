package analyticscore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func Tools() []Tool {
	descriptions := []struct {
		name, description string
		category          bool
	}{
		{"get_cycle_overview", "Read a salary cycle's cashflow, spending shape and descriptive household attribution. Null cycle_start selects the current cycle; returned cycles list available starts.", false},
		{"get_cycle_changes", "Read expense, income and net cashflow changes against the previous completed comparable cycle and previous-three-cycle median. ELAPSED_DAYS compares exact measured prefixes, not full cycles. Separate previous_full_cycle amounts/deltas provide full historical context, not an equal-day baseline. Null relative deltas are unavailable, never 100%. Returns up to 20 category changes sorted by absolute full-cycle delta when available, otherwise comparable delta and request-local category refs.", false},
		{"get_category_drivers", "Read one category's exact changes and concentration context. Use a category_ref issued by get_cycle_changes for this cycle.", true},
		{"get_merchant_drivers", "Read up to 10 merchant drivers, sorted by absolute full-cycle delta when available, otherwise comparable delta. Null category_ref selects the whole cycle; otherwise use a category_ref issued by get_cycle_changes.", true},
		{"get_supporting_transactions", "Read up to 10 supporting confirmed expense/refund transactions for a category_ref issued by get_cycle_changes. No raw evidence or canonical IDs.", true},
		{"get_savings_reconciliation", "Read confirmed surplus, allocated savings, destinations and unallocated residual. Transfers are not household expense.", false},
		{"get_wealth_reconciliation", "Read actual Wealth snapshot dates, ages, net-worth movement, confirmed cashflow and valuation/other residual. These are observation-interval facts, not invented cycle-end balances.", false},
		{"get_cycle_data_quality", "Read scoped CASHFLOW, PROCESSING and WEALTH_RECONCILIATION findings and deterministic action paths, not an AI confidence score. Numeric data_completeness measures cashflow coverage only. Wealth comparison caveats neither invalidate confirmed cashflow nor imply active Inbox items.", false},
	}
	out := make([]Tool, 0, len(descriptions))
	for _, d := range descriptions {
		props := map[string]any{"cycle_start": map[string]any{"type": []string{"string", "null"}, "description": "Confirmed salary cycle start YYYY-MM-DD, or null for current cycle."}}
		required := []string{"cycle_start"}
		if d.category {
			props["category_ref"] = map[string]any{"type": []string{"string", "null"}}
			required = append(required, "category_ref")
		}
		out = append(out, Tool{d.name, d.description, map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}})
	}
	return out
}

func IsRead(name string) bool {
	for _, tool := range Tools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

type Args struct {
	CycleStart  *string `json:"cycle_start"`
	CategoryRef *string `json:"category_ref,omitempty"`
}

func DecodeArgs(name string, raw json.RawMessage) (Args, error) {
	var definition *Tool
	for _, tool := range Tools() {
		if tool.Name == name {
			definition = &tool
			break
		}
	}
	if definition == nil {
		return Args{}, fmt.Errorf("unknown analytical READ tool %q", name)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return Args{}, fmt.Errorf("invalid analytical arguments")
	}
	props := definition.Parameters["properties"].(map[string]any)
	for key := range keys {
		if _, ok := props[key]; !ok {
			return Args{}, fmt.Errorf("unknown analytical argument %q", key)
		}
	}
	for _, key := range definition.Parameters["required"].([]string) {
		if _, ok := keys[key]; !ok {
			return Args{}, fmt.Errorf("missing analytical argument %q", key)
		}
	}
	var args Args
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Args{}, fmt.Errorf("invalid analytical arguments")
	}
	if args.CycleStart != nil {
		if _, err := time.Parse("2006-01-02", *args.CycleStart); err != nil {
			return Args{}, fmt.Errorf("invalid analytical cycle_start")
		}
	}
	if args.CategoryRef != nil && *args.CategoryRef == "" {
		return Args{}, fmt.Errorf("empty analytical category_ref")
	}
	if (name == "get_category_drivers" || name == "get_supporting_transactions") && args.CategoryRef == nil {
		return Args{}, fmt.Errorf("category_ref required")
	}
	return args, nil
}

type Session struct {
	pool                 *pgxpool.Pool
	household            string
	currentSourceEventID string
	now                  time.Time
	mu                   sync.Mutex
	facts                map[string]Facts
	issued               map[string]map[string]int
}

var ErrCategoryRefNotIssued = errors.New("category_ref not issued for selected cycle")

func NewSession(pool *pgxpool.Pool, household string, now time.Time, currentSourceEventID string) *Session {
	return &Session{pool: pool, household: household, now: now, currentSourceEventID: currentSourceEventID, facts: map[string]Facts{}, issued: map[string]map[string]int{}}
}

func (s *Session) Read(ctx context.Context, name string, raw json.RawMessage) (map[string]any, error) {
	args, err := DecodeArgs(name, raw)
	if err != nil {
		return nil, err
	}
	key := ""
	if args.CycleStart != nil {
		key = *args.CycleStart
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.household == "" {
		return nil, fmt.Errorf("household required")
	}
	f, ok := s.facts[key]
	if !ok {
		f, err = load(ctx, s.pool, s.household, key, s.now, 0, s.currentSourceEventID)
		if err != nil {
			return nil, err
		}
		s.facts[key] = f
		s.facts[f.Period.Start] = f
	}
	periodKey := f.Period.Start
	result := map[string]any{"period": f.Period, "facts_version": f.Version, "generated_at": f.GeneratedAt, "data_completeness": f.Completeness(), "data_completeness_scope": "CASHFLOW"}
	var category *reviewCategory
	if args.CategoryRef != nil {
		index, exists := s.issued[periodKey][*args.CategoryRef]
		if !exists {
			return nil, ErrCategoryRefNotIssued
		}
		category = &f.Categories[index]
	}
	switch name {
	case "get_cycle_overview":
		result["cashflow"], result["spending_shape"], result["cycles"] = f.Cashflow, f.SpendingShape, f.Cycles
		result["member_attribution"] = modelValues(memberValues(f.Members), "member")
	case "get_cycle_changes":
		result["comparison"] = map[string]any{"mode": f.Comparison.Mode, "previous": f.Comparison.Previous, "previous_full_cycle": f.Comparison.PreviousFullCycle, "eligible_cycles": f.Comparison.EligibleCycles, "median3_available": f.Comparison.Median3Available,
			"expense": modelChange(f.Comparison.Expense, "cashflow.expense"), "income": modelChange(f.Comparison.Income, "cashflow.income"), "net_cashflow": modelChange(f.Comparison.NetCashflow, "cashflow.net")}
		items := []map[string]any{}
		if s.issued[periodKey] == nil {
			s.issued[periodKey] = map[string]int{}
		}
		for i, c := range f.Categories {
			if i == 20 {
				break
			}
			ref := "category." + strconv.Itoa(i+1)
			s.issued[periodKey][ref] = i
			items = append(items, modelChange(c.reviewChange, ref))
		}
		result["categories"], result["total_categories"] = items, len(f.Categories)
	case "get_category_drivers":
		result["category"] = modelChange(category.reviewChange, *args.CategoryRef)
		result["merchant_driver_count"], result["supporting_transaction_count"] = len(category.Merchants), len(category.Transactions)
	case "get_merchant_drivers":
		merchants := f.Merchants
		prefix := "merchant"
		if category != nil {
			merchants, prefix = category.Merchants, *args.CategoryRef+".merchant"
		}
		items := []map[string]any{}
		for i, merchant := range merchants {
			items = append(items, modelChange(merchant, prefix+"."+strconv.Itoa(i+1)))
		}
		result["merchants"] = items
	case "get_supporting_transactions":
		items := []map[string]any{}
		for i, t := range category.Transactions {
			items = append(items, map[string]any{"ref": *args.CategoryRef + ".transaction." + strconv.Itoa(i+1), "transaction_at": t.At, "type": t.Type, "amount": t.Amount, "merchant": untrustedLedger(t.Merchant)})
		}
		result["transactions"] = items
	case "get_savings_reconciliation":
		result["cashflow"] = f.Cashflow
		result["destinations"] = modelValues(f.Destinations, "savings.destination")
	case "get_wealth_reconciliation":
		result["wealth"] = map[string]any{"current": modelSnapshot(f.Wealth.Current), "previous": modelSnapshot(f.Wealth.Previous), "net_worth_change": f.Wealth.Change, "confirmed_cashflow": f.Wealth.Cashflow, "valuation_and_other_change": f.Wealth.Other}
	case "get_cycle_data_quality":
		result["blockers"] = f.Quality
	}
	return result, nil
}

const untrustedLedgerTag = "untrusted_ledger_text"

var untrustedTagPattern = regexp.MustCompile(`(?i)<(s*/?s*(?:untrusted_|evidence))`)

func untrustedLedger(value string) string {
	return "<" + untrustedLedgerTag + ">" + untrustedTagPattern.ReplaceAllString(value, "&lt;$1") + "</" + untrustedLedgerTag + ">"
}

func modelChange(c reviewChange, ref string) map[string]any {
	return map[string]any{"ref": ref, "name": untrustedLedger(c.Name), "current": c.Amount, "count": c.Count, "previous": c.Previous, "delta_vs_previous": c.Delta, "relative_delta_vs_previous": c.Relative,
		"previous_full_cycle": c.PreviousFull, "delta_vs_previous_full_cycle": c.DeltaFull, "relative_delta_vs_previous_full_cycle": c.RelativeFull,
		"median3": c.Median, "delta_vs_median3": c.DeltaMedian, "relative_delta_vs_median3": c.RelativeMedian, "contribution_to_expense_change": c.Contribution, "share_of_expense": c.Share}
}

func modelValues(values []reviewValue, prefix string) []map[string]any {
	out := []map[string]any{}
	for i, v := range values {
		out = append(out, map[string]any{"ref": prefix + "." + strconv.Itoa(i+1), "name": untrustedLedger(v.Name), "amount": v.Amount, "count": v.Count})
	}
	return out
}

func modelSnapshot(s *reviewSnapshot) any {
	if s == nil {
		return nil
	}
	return map[string]any{"observed_at": s.At, "net_worth": s.NetWorth, "age_days": s.AgeDays}
}
