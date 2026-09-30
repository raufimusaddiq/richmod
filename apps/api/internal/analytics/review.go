package analytics

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
	"github.com/raufimusaddiq/richmod/apps/api/internal/financialmath"
)

var errCycleNotFound = errors.New("confirmed salary cycle not found")

type reviewPeriod struct {
	Kind          string  `json:"kind"`
	Start         string  `json:"start"`
	End           *string `json:"end"` // exclusive; unknown for an active salary cycle
	MeasuredUntil string  `json:"measuredUntil"`
	State         string  `json:"state"`
	Configured    bool    `json:"configured"`
}

type reviewCashflow struct {
	Income       string `json:"income"`
	GrossExpense string `json:"grossExpense"`
	Refund       string `json:"refund"`
	Expense      string `json:"expense"`
	Net          string `json:"netCashflow"`
	Allocated    string `json:"savingsAllocated"`
	Unallocated  string `json:"unallocatedSurplus"`
}

type reviewValue struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
	Count  int    `json:"count"`
}

type reviewChange struct {
	reviewValue
	Previous       *string `json:"previous"`
	Delta          *string `json:"deltaVsPrevious"`
	Relative       *string `json:"relativeDeltaVsPrevious"`
	Median         *string `json:"median3"`
	DeltaMedian    *string `json:"deltaVsMedian3"`
	RelativeMedian *string `json:"relativeDeltaVsMedian3"`
	Contribution   *string `json:"contributionToExpenseChange"`
	Share          *string `json:"shareOfExpense"`
}

type reviewCategory struct {
	reviewChange
	Merchants    []reviewChange      `json:"merchants"`
	Transactions []reviewTransaction `json:"transactions"`
}

type reviewTransaction struct {
	ID       string    `json:"id"`
	At       time.Time `json:"transactionAt"`
	Type     string    `json:"type"`
	Amount   string    `json:"amount"`
	Merchant string    `json:"merchant"`
}

type reviewBlocker struct {
	Kind   string  `json:"kind"`
	Count  int     `json:"count"`
	Amount *string `json:"amount,omitempty"`
	Impact string  `json:"impact"`
	Action string  `json:"action"`
}

type reviewSnapshot struct {
	ID       string    `json:"id"`
	At       time.Time `json:"observedAt"`
	NetWorth string    `json:"netWorth"`
	AgeDays  int       `json:"ageDays"`
	accounts []string
}

type reviewWealth struct {
	Current  *reviewSnapshot `json:"current"`
	Previous *reviewSnapshot `json:"previous"`
	Change   *string         `json:"netWorthChange"`
	Cashflow *string         `json:"confirmedCashflow"`
	Other    *string         `json:"valuationAndOtherChange"`
}

type cycleReview struct {
	Version     string         `json:"version"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Period      reviewPeriod   `json:"period"`
	Cycles      []reviewPeriod `json:"cycles"`
	Comparison  struct {
		Mode             string        `json:"mode"`
		Previous         *reviewPeriod `json:"previous"`
		EligibleCycles   int           `json:"eligibleCycles"`
		Median3Available bool          `json:"median3Available"`
		Expense          reviewChange  `json:"expense"`
		Income           reviewChange  `json:"income"`
		NetCashflow      reviewChange  `json:"netCashflow"`
	} `json:"comparison"`
	Cashflow      reviewCashflow      `json:"cashflow"`
	Daily         []map[string]string `json:"daily"`
	SpendingShape struct {
		Days        int     `json:"days"`
		Average     string  `json:"averageDailyExpense"`
		PeakDay     *string `json:"peakDay"`
		PeakExpense string  `json:"peakExpense"`
		PeakShare   *string `json:"peakShareOfExpense"`
		ZeroDays    int     `json:"zeroSpendDays"`
	} `json:"spendingShape"`
	Categories   []reviewCategory `json:"categoryChanges"`
	Merchants    []reviewChange   `json:"merchantDrivers"`
	Members      []reviewValue    `json:"memberAttribution"`
	Destinations []reviewValue    `json:"savingsDestinations"`
	Wealth       reviewWealth     `json:"wealth"`
	Quality      []reviewBlocker  `json:"dataQuality"`
}

// CycleReview is a read-only, single-snapshot financial fact endpoint. Models,
// financial mutations and analytical significance are deliberately absent.
func (h *Handler) CycleReview(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start := r.URL.Query().Get("cycle_start")
	if start != "" {
		if _, err := time.Parse("2006-01-02", start); err != nil {
			writeJSON(w, 400, map[string]string{"error": "cycle_start must be YYYY-MM-DD"})
			return
		}
	}
	facts, err := h.loadCycleReview(r.Context(), household, start)
	if errors.Is(err, errCycleNotFound) {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to calculate cycle review"})
		return
	}
	writeJSON(w, 200, facts)
}

type cycleMeasure struct {
	period                reviewPeriod
	cash                  reviewCashflow
	categories, merchants map[string]reviewValue
	categoryMerchants     map[string]map[string]reviewValue
}

func newCycleMeasure(period reviewPeriod) cycleMeasure {
	return cycleMeasure{period: period, cash: reviewCashflow{Income: "0", GrossExpense: "0", Refund: "0", Expense: "0", Net: "0", Allocated: "0", Unallocated: "0"}, categories: map[string]reviewValue{}, merchants: map[string]reviewValue{}, categoryMerchants: map[string]map[string]reviewValue{}}
}

func (h *Handler) loadCycleReview(ctx context.Context, household, selected string) (cycleReview, error) {
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return cycleReview{}, err
	}
	defer tx.Rollback(ctx)
	now := cycleNow(h)().In(clock.HouseholdLocation())
	periods, configured, err := reviewPeriods(ctx, tx, household, now)
	if err != nil {
		return cycleReview{}, err
	}
	facts := cycleReview{Version: "cycle-review-v1", GeneratedAt: now, Cycles: periods, Daily: []map[string]string{}, Categories: []reviewCategory{}, Merchants: []reviewChange{}, Members: []reviewValue{}, Destinations: []reviewValue{}, Quality: []reviewBlocker{}}
	index := 0
	if selected != "" {
		index = -1
		for i, p := range periods {
			if p.Start == selected {
				index = i
				break
			}
		}
		if index < 0 {
			return facts, errCycleNotFound
		}
	}
	if len(periods) == 0 {
		if selected != "" {
			return facts, errCycleNotFound
		}
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
		end := start.AddDate(0, 1, 0).Format("2006-01-02")
		periods = []reviewPeriod{{"CALENDAR_MONTH", start.Format("2006-01-02"), &end, now.AddDate(0, 0, 1).Format("2006-01-02"), "ACTIVE", configured}}
		facts.block("MISSING_SALARY_ANCHOR", 1, nil, "/settings")
	}
	facts.Period = periods[index]
	measures := []cycleMeasure{newCycleMeasure(facts.Period)}
	facts.Comparison.Mode = "FULL_CYCLE"
	if facts.Period.State == "ACTIVE" {
		facts.Comparison.Mode = "ELAPSED_DAYS"
	}
	for _, p := range periods[index+1:] {
		if p.State != "CLOSED" {
			continue
		}
		if facts.Period.State == "ACTIVE" {
			elapsed := daysBetween(facts.Period.Start, facts.Period.MeasuredUntil)
			if daysBetween(p.Start, p.MeasuredUntil) < elapsed {
				continue
			}
			start, _ := time.ParseInLocation("2006-01-02", p.Start, clock.HouseholdLocation())
			p.MeasuredUntil = start.AddDate(0, 0, elapsed).Format("2006-01-02")
		}
		measures = append(measures, newCycleMeasure(p))
		if len(measures) == 4 {
			break
		}
	}
	facts.Comparison.EligibleCycles = len(measures) - 1
	facts.Comparison.Median3Available = len(measures) == 4
	if len(measures) > 1 {
		p := measures[1].period
		facts.Comparison.Previous = &p
	}
	if err := loadReviewMeasures(ctx, tx, household, measures, &facts); err != nil {
		return facts, err
	}
	for i := range measures {
		cash, err := financialmath.CalculateCashflow(measures[i].cash.Income, measures[i].cash.GrossExpense, measures[i].cash.Refund)
		if err != nil {
			return facts, err
		}
		measures[i].cash.Expense, measures[i].cash.Net = cash.NetExpense, cash.Surplus
		measures[i].cash.Unallocated = subtract(cash.Surplus, measures[i].cash.Allocated)
	}
	facts.Cashflow = measures[0].cash
	facts.Comparison.Expense = cashChange(measures, func(c reviewCashflow) string { return c.Expense })
	facts.Comparison.Income = cashChange(measures, func(c reviewCashflow) string { return c.Income })
	facts.Comparison.NetCashflow = cashChange(measures, func(c reviewCashflow) string { return c.Net })
	for _, c := range changes(measures, func(m cycleMeasure) map[string]reviewValue { return m.categories }) {
		category := reviewCategory{reviewChange: c, Merchants: []reviewChange{}, Transactions: []reviewTransaction{}}
		category.Merchants = changes(measures, func(m cycleMeasure) map[string]reviewValue { return m.categoryMerchants[c.ID] })
		if len(category.Merchants) > 10 {
			category.Merchants = category.Merchants[:10]
		}
		facts.Categories = append(facts.Categories, category)
	}
	facts.Merchants = changes(measures, func(m cycleMeasure) map[string]reviewValue { return m.merchants })
	if len(facts.Merchants) > 10 {
		facts.Merchants = facts.Merchants[:10]
	}
	if err := loadReviewTransactions(ctx, tx, household, &facts); err != nil {
		return facts, err
	}
	if err := loadReviewQuality(ctx, tx, household, &facts); err != nil {
		return facts, err
	}
	if err := loadReviewWealth(ctx, tx, household, now, &facts); err != nil {
		return facts, err
	}
	return facts, tx.Commit(ctx)
}

func (f *cycleReview) block(kind string, count int, amount *string, action string) {
	if count > 0 {
		f.Quality = append(f.Quality, reviewBlocker{kind, count, amount, "ANALYSIS_PARTIAL", action})
	}
}

func daysBetween(start, end string) int {
	a, _ := time.Parse("2006-01-02", start)
	b, _ := time.Parse("2006-01-02", end)
	return int(b.Sub(a).Hours() / 24)
}

func valuePointer(s string) *string { return &s }

func change(current string, previous, median *string) reviewChange {
	c := reviewChange{reviewValue: reviewValue{Amount: current}, Previous: previous, Median: median}
	if previous != nil {
		c.Delta = valuePointer(subtract(current, *previous))
		c.Relative = positiveRatio(*c.Delta, *previous)
	}
	if median != nil {
		c.DeltaMedian = valuePointer(subtract(current, *median))
		c.RelativeMedian = positiveRatio(*c.DeltaMedian, *median)
	}
	return c
}

func positiveRatio(n, d string) *string {
	if v, ok := ratio(n, d); ok {
		return &v
	}
	return nil
}

func median3(values []string) *string {
	if len(values) != 3 {
		return nil
	}
	copyValues := append([]string(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool {
		a, _ := new(big.Int).SetString(copyValues[i], 10)
		b, _ := new(big.Int).SetString(copyValues[j], 10)
		return a.Cmp(b) < 0
	})
	return &copyValues[1]
}

func cashChange(measures []cycleMeasure, amount func(reviewCashflow) string) reviewChange {
	var previous *string
	history := []string{}
	for _, m := range measures[1:] {
		history = append(history, amount(m.cash))
	}
	if len(history) > 0 {
		previous = &history[0]
	}
	return change(amount(measures[0].cash), previous, median3(history))
}

func changes(measures []cycleMeasure, values func(cycleMeasure) map[string]reviewValue) []reviewChange {
	names := map[string]reviewValue{}
	for i := len(measures) - 1; i >= 0; i-- {
		for id, v := range values(measures[i]) {
			names[id] = v
		}
	}
	out := []reviewChange{}
	for id, name := range names {
		current, ok := values(measures[0])[id]
		if !ok {
			current = reviewValue{ID: name.ID, Name: name.Name, Amount: "0"}
		}
		history := []string{}
		for _, m := range measures[1:] {
			v, ok := values(m)[id]
			if !ok {
				v.Amount = "0"
			}
			history = append(history, v.Amount)
		}
		var previous *string
		if len(history) > 0 {
			previous = &history[0]
		}
		c := change(current.Amount, previous, median3(history))
		c.reviewValue = current
		c.Share = positiveRatio(current.Amount, measures[0].cash.Expense)
		if c.Delta != nil {
			den, _ := new(big.Int).SetString(subtract(measures[0].cash.Expense, measures[1].cash.Expense), 10)
			if den.Sign() != 0 {
				num, _ := new(big.Int).SetString(*c.Delta, 10)
				c.Contribution = valuePointer(new(big.Rat).SetFrac(num, den).FloatString(4))
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Amount, out[j].Amount
		if out[i].Delta != nil {
			a, b = *out[i].Delta, *out[j].Delta
		}
		x, _ := new(big.Int).SetString(a, 10)
		y, _ := new(big.Int).SetString(b, 10)
		if c := x.Abs(x).Cmp(y.Abs(y)); c != 0 {
			return c > 0
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func compareAmounts(a, b string) int {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return x.Cmp(y)
}

func amountPerDay(amount string, days int) string {
	if days == 0 {
		return "0"
	}
	x, _ := new(big.Int).SetString(amount, 10)
	return new(big.Rat).SetFrac(x, big.NewInt(int64(days))).FloatString(2)
}

func orderedValues(values map[string]reviewValue) []reviewValue {
	out := []reviewValue{}
	for _, v := range values {
		out = append(out, v)
	}
	// Attribution is descriptive, not a ranking of household members.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
