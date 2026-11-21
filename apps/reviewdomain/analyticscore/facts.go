package analyticscore

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/clock"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
)

// ErrCycleNotFound reports a requested cycle start with no confirmed salary anchor.
var ErrCycleNotFound = errors.New("confirmed salary cycle not found")

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
	PreviousFull   *string `json:"previousFullCycle"`
	DeltaFull      *string `json:"deltaVsPreviousFullCycle"`
	RelativeFull   *string `json:"relativeDeltaVsPreviousFullCycle"`
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
	Scope  string  `json:"scope"`
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

// historyCycle is one ledger column: whole-cycle totals for the cycle-to-cycle view.
// Closed cycles are measured to their exclusive end, the active cycle to measuredUntil.
type historyCycle struct {
	Start            string  `json:"start"`
	End              *string `json:"end"`
	MeasuredUntil    string  `json:"measuredUntil"`
	State            string  `json:"state"`
	Income           string  `json:"income"`
	GrossExpense     string  `json:"grossExpense"`
	Refund           string  `json:"refund"`
	Expense          string  `json:"expense"`
	Net              string  `json:"netCashflow"`
	SavingsAllocated string  `json:"savingsAllocated"`
	// ExpenseDelta is expense minus the preceding history entry's expense; null
	// for the first entry. It is server arithmetic for the ledger's change row.
	ExpenseDelta *string `json:"expenseDelta"`
}

type historyCategory struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Amounts []string `json:"amounts"`
	// DeltaVsPrevious is the selected cycle's amount minus the cycle before it in
	// this matrix; null when the selected cycle has no earlier column.
	DeltaVsPrevious *string `json:"deltaVsPrevious"`
}

type historyOther struct {
	Amounts         []string `json:"amounts"`
	DeltaVsPrevious *string  `json:"deltaVsPrevious"`
}

// categoryMatrix is one category x cycle matrix. Rows are the top categories by
// net expense over the window; Other holds the rest, and Totals is each cycle's
// net expense, so rows + Other = Totals in every column.
type categoryMatrix struct {
	CycleStarts []string `json:"cycleStarts"`
	// SelectedIndex is the selected cycle's column; null when it is not in the window.
	SelectedIndex        *int              `json:"selectedIndex"`
	Rows                 []historyCategory `json:"rows"`
	Other                historyOther      `json:"other"`
	Totals               []string          `json:"totals"`
	TotalDeltaVsPrevious *string           `json:"totalDeltaVsPrevious"`
}

// sameDayMatrix cuts every cycle at the same elapsed day-of-cycle as the active
// selected cycle (a shorter cycle is cut at its own end), so no partial cycle is
// compared with a full one. It uses the same rows as the full matrix.
type sameDayMatrix struct {
	ElapsedDays int `json:"elapsedDays"`
	// MeasuredDays is how many days each column actually covers: ElapsedDays, or
	// fewer for a cycle that ended sooner (kept whole, so the UI can say "hari 1-N").
	MeasuredDays []int `json:"measuredDays"`
	categoryMatrix
}

// categoryHistory is the full-cycle matrix plus, while the selected cycle is
// ACTIVE, the same-day view (null otherwise).
type categoryHistory struct {
	categoryMatrix
	SameDay *sameDayMatrix `json:"sameDay"`
}

func emptyMatrix() categoryMatrix {
	return categoryMatrix{CycleStarts: []string{}, Rows: []historyCategory{}, Other: historyOther{Amounts: []string{}}, Totals: []string{}}
}

func emptyCategoryHistory() categoryHistory {
	return categoryHistory{categoryMatrix: emptyMatrix()}
}

// merchantRankRow is one payee in the amount-ranked merchant list. Kind is
// MERCHANT (resolved merchant, ID set), COUNTERPARTY (unresolved named payee) or
// NONE (no merchant and no counterparty, shown as "Tanpa merchant").
type merchantRankRow struct {
	reviewValue
	Kind     string  `json:"kind"`
	Previous *string `json:"previous"`
	Delta    *string `json:"deltaVsPrevious"`
	Share    *string `json:"shareOfExpense"`
}

type merchantRankOther struct {
	Amount        string  `json:"amount"`
	Count         int     `json:"count"`
	MerchantCount int     `json:"merchantCount"`
	Previous      *string `json:"previous"`
	Delta         *string `json:"deltaVsPrevious"`
}

type merchantRankTotal struct {
	Amount   string  `json:"amount"`
	Count    int     `json:"count"`
	Previous *string `json:"previous"`
	Delta    *string `json:"deltaVsPrevious"`
}

// merchantRanking lists payees by net amount, largest first. Previous follows
// the comparison mode: the previous eligible cycle at the same elapsed day while
// ACTIVE, the previous full cycle once CLOSED. Rows + Other = Total = cashflow.expense.
type merchantRanking struct {
	Mode  string            `json:"mode"`
	Rows  []merchantRankRow `json:"rows"`
	Other merchantRankOther `json:"other"`
	Total merchantRankTotal `json:"total"`
}

const merchantRankingRows = 10

// reviewMember is one recorder in the household attribution. It is descriptive,
// not a ranking: the automatic bucket comes first, then members by name.
type reviewMember struct {
	reviewValue
	Kind  string  `json:"kind"` // AUTOMATIC | MEMBER
	Share *string `json:"shareOfExpense"`
}

type Facts struct {
	Version     string         `json:"version"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Period      reviewPeriod   `json:"period"`
	Cycles      []reviewPeriod `json:"cycles"`
	// History and CategoryHistory are additive ledger series; they are never
	// forwarded to analytical tools (tools use explicit projections).
	History         []historyCycle  `json:"history"`
	CategoryHistory categoryHistory `json:"categoryHistory"`
	// Pace carries the previous cycle's and the 3-cycle median's cumulative curves
	// for the pace chart. Like History, it is never forwarded to analytical tools.
	Pace       paceBaselines `json:"pace"`
	Comparison struct {
		Mode              string        `json:"mode"`
		Previous          *reviewPeriod `json:"previous"`
		PreviousFullCycle *reviewPeriod `json:"previousFullCycle"`
		EligibleCycles    int           `json:"eligibleCycles"`
		Median3Available  bool          `json:"median3Available"`
		Expense           reviewChange  `json:"expense"`
		Income            reviewChange  `json:"income"`
		NetCashflow       reviewChange  `json:"netCashflow"`
	} `json:"comparison"`
	Cashflow      reviewCashflow      `json:"cashflow"`
	Daily         []map[string]string `json:"daily"`
	SpendingShape struct {
		Days    int    `json:"days"`
		Average string `json:"averageDailyExpense"`
		// AverageRounded is the average in whole rupiah, half away from zero, so the web never rounds money.
		AverageRounded string  `json:"averageDailyExpenseRounded"`
		PeakDay        *string `json:"peakDay"`
		PeakExpense    string  `json:"peakExpense"`
		PeakShare      *string `json:"peakShareOfExpense"`
		ZeroDays       int     `json:"zeroSpendDays"`
	} `json:"spendingShape"`
	CategoryRanking categoryRanking  `json:"categoryRanking"`
	Categories      []reviewCategory `json:"categoryChanges"`
	Merchants       []reviewChange   `json:"merchantDrivers"`
	// MerchantRanking is the amount-ranked payee list for the cycle view.
	MerchantRanking merchantRanking `json:"merchantRanking"`
	Members         []reviewMember  `json:"memberAttribution"`
	Destinations    []reviewValue   `json:"savingsDestinations"`
	Wealth          reviewWealth    `json:"wealth"`
	Quality         []reviewBlocker `json:"dataQuality"`
}

// Completeness retains ADR-015's coverage gate on gross confirmed expense so refunds
// cannot inflate category coverage. Open reviews apply the 0.90 coverage factor; this
// measures data quality, never noteworthiness.
func (f Facts) Completeness() string {
	gross, _ := new(big.Int).SetString(f.Cashflow.GrossExpense, 10)
	if gross == nil {
		return "0.0000"
	}
	uncategorized := "0"
	reviews := 0
	for _, b := range f.Quality {
		if b.Kind == "OPEN_REVIEWS" {
			reviews += b.Count
		}
		if b.Kind == "UNCATEGORIZED_EXPENSE" && b.Amount != nil {
			uncategorized = *b.Amount
		}
	}
	if gross.Sign() <= 0 {
		if reviews > 0 {
			return "0.5000"
		}
		return "1.0000"
	}
	covered, _ := new(big.Int).SetString(financialmath.Subtract(gross.String(), uncategorized), 10)
	ratio := new(big.Rat).SetFrac(covered, gross)
	if reviews > 0 {
		ratio.Mul(ratio, big.NewRat(9, 10))
	}
	if ratio.Sign() < 0 {
		ratio.SetInt64(0)
	}
	if ratio.Cmp(big.NewRat(1, 1)) > 0 {
		ratio.SetInt64(1)
	}
	return ratio.FloatString(4)
}

type cycleMeasure struct {
	period                reviewPeriod
	cash                  reviewCashflow
	categories, merchants map[string]reviewValue
	categoryMerchants     map[string]map[string]reviewValue
	dailyNet              map[string]string // YYYY-MM-DD (Jakarta) -> net expense that day
}

func newCycleMeasure(period reviewPeriod) cycleMeasure {
	return cycleMeasure{period: period, dailyNet: map[string]string{}, cash: reviewCashflow{Income: "0", GrossExpense: "0", Refund: "0", Expense: "0", Net: "0", Allocated: "0", Unallocated: "0"}, categories: map[string]reviewValue{}, merchants: map[string]reviewValue{}, categoryMerchants: map[string]map[string]reviewValue{}}
}

const (
	// DefaultHistory and MaxHistory bound the ledger window (cycles).
	DefaultHistory = 6
	MaxHistory     = 12
)

// Load computes one household's authoritative cycle facts in a read-only
// repeatable-read snapshot, without the multi-cycle history series. Callers own
// household authorization.
func Load(ctx context.Context, pool *pgxpool.Pool, household, selected string, now time.Time) (Facts, error) {
	return LoadWithHistory(ctx, pool, household, selected, now, 0)
}

// LoadWithHistory is Load plus the cycle-to-cycle history series for up to
// `history` cycles (0 disables it, at most MaxHistory).
func LoadWithHistory(ctx context.Context, pool *pgxpool.Pool, household, selected string, now time.Time, history int) (Facts, error) {
	return load(ctx, pool, household, selected, now, history, "")
}

// load excludes only the current server-owned source from unfinished processing.
func load(ctx context.Context, pool *pgxpool.Pool, household, selected string, now time.Time, history int, currentSourceEventID string) (Facts, error) {
	if history < 0 || history > MaxHistory {
		return Facts{}, errors.New("history must be 0 to 12")
	}
	if pool == nil {
		return Facts{}, errors.New("database pool required")
	}
	if household == "" {
		return Facts{}, errors.New("household required")
	}
	if selected != "" {
		if _, err := time.Parse("2006-01-02", selected); err != nil {
			return Facts{}, errors.New("cycle_start must be YYYY-MM-DD")
		}
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Facts{}, err
	}
	defer tx.Rollback(ctx)
	now = now.In(clock.HouseholdLocation())
	periods, configured, err := reviewPeriods(ctx, tx, household, now)
	if err != nil {
		return Facts{}, err
	}
	facts := Facts{Version: "cycle-review-v1", GeneratedAt: now, Cycles: periods, History: []historyCycle{}, CategoryHistory: emptyCategoryHistory(), CategoryRanking: emptyCategoryRanking(), Daily: []map[string]string{}, Categories: []reviewCategory{}, Merchants: []reviewChange{}, MerchantRanking: emptyMerchantRanking(), Members: []reviewMember{}, Destinations: []reviewValue{}, Quality: []reviewBlocker{}}
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
			return facts, ErrCycleNotFound
		}
	}
	if len(periods) == 0 {
		if selected != "" {
			return facts, ErrCycleNotFound
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
	for offset, p := range periods[index+1:] {
		if p.State != "CLOSED" {
			continue
		}
		if facts.Period.State == "ACTIVE" {
			// A closed cycle shorter than the elapsed days is kept whole (measured
			// to its own end) instead of skipped; skipping it made an older, longer
			// cycle masquerade as "previous" and disagreed with the history matrix.
			p = sameDayPeriod(p, daysBetween(facts.Period.Start, facts.Period.MeasuredUntil))
			facts.Cycles[index+1+offset].MeasuredUntil = p.MeasuredUntil
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
	// Full-cycle context is separate from eligible equal-day history and median.
	historyCount, fullIndex := len(measures), -1
	if index+1 < len(periods) && periods[index+1].State == "CLOSED" {
		p := periods[index+1]
		p.MeasuredUntil = *p.End
		facts.Comparison.PreviousFullCycle = &p
		for i := 1; i < len(measures); i++ {
			if measures[i].period.Start == p.Start && measures[i].period.MeasuredUntil == p.MeasuredUntil {
				fullIndex = i
				break
			}
		}
		if fullIndex < 0 {
			fullIndex = len(measures)
			measures = append(measures, newCycleMeasure(p))
		}
	}
	var window, sameDay []int
	if history > 0 && facts.Period.Kind == "SALARY_CYCLE" {
		cycles := historyWindow(periods, index, history)
		measures, window = historyMeasures(measures, cycles)
		if facts.Period.State == "ACTIVE" {
			measures, sameDay = historyMeasures(measures, sameDayWindow(cycles, daysBetween(facts.Period.Start, facts.Period.MeasuredUntil)))
		}
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
		measures[i].cash.Unallocated = financialmath.Subtract(cash.Surplus, measures[i].cash.Allocated)
	}
	if len(window) > 0 {
		facts.History, facts.CategoryHistory = buildHistory(measures, window, sameDay, facts.Period.Start)
	}
	var full *cycleMeasure
	if fullIndex >= 0 {
		full = &measures[fullIndex]
	}
	measures = measures[:historyCount]
	facts.MerchantRanking = buildMerchantRanking(measures, facts.Comparison.Mode)
	facts.Pace = buildPace(full, measures[1:], facts.Comparison.Median3Available)
	facts.Cashflow = measures[0].cash
	var rankBase *cycleMeasure
	if facts.Comparison.Mode == "ELAPSED_DAYS" {
		if len(measures) > 1 {
			rankBase = &measures[1]
		}
	} else {
		rankBase = full
	}
	facts.CategoryRanking = buildCategoryRanking(measures[0], rankBase, facts.Comparison.Mode)
	facts.Comparison.Expense = cashChange(measures, func(c reviewCashflow) string { return c.Expense }, full)
	facts.Comparison.Income = cashChange(measures, func(c reviewCashflow) string { return c.Income }, full)
	facts.Comparison.NetCashflow = cashChange(measures, func(c reviewCashflow) string { return c.Net }, full)
	for _, c := range changes(measures, func(m cycleMeasure) map[string]reviewValue { return m.categories }, full) {
		category := reviewCategory{reviewChange: c, Merchants: []reviewChange{}, Transactions: []reviewTransaction{}}
		category.Merchants = changes(measures, func(m cycleMeasure) map[string]reviewValue { return m.categoryMerchants[c.ID] }, full)
		if len(category.Merchants) > 10 {
			category.Merchants = category.Merchants[:10]
		}
		facts.Categories = append(facts.Categories, category)
	}
	facts.Merchants = changes(measures, func(m cycleMeasure) map[string]reviewValue { return m.merchants }, full)
	if len(facts.Merchants) > 10 {
		facts.Merchants = facts.Merchants[:10]
	}
	if err := loadReviewTransactions(ctx, tx, household, &facts); err != nil {
		return facts, err
	}
	if err := loadReviewQuality(ctx, tx, household, currentSourceEventID, &facts); err != nil {
		return facts, err
	}
	if err := loadReviewWealth(ctx, tx, household, now, &facts); err != nil {
		return facts, err
	}
	return facts, tx.Commit(ctx)
}

// paceBaselines are per-day cumulative net-expense curves; index i is day i+1 of
// the cycle. PreviousFullCycle is the previous closed cycle in full. Median3 is the
// per-day median of the three eligible cycles over the days all three share (the
// elapsed days while the selected cycle runs). Each is null when it does not exist.
type paceBaselines struct {
	PreviousFullCycle []string `json:"previousFullCycle"`
	Median3           []string `json:"median3"`
}

// cumulativeSeries is the running net expense for each day from the cycle start up
// to its measured cutoff.
func cumulativeSeries(m cycleMeasure) []string {
	start, startErr := time.ParseInLocation("2006-01-02", m.period.Start, clock.HouseholdLocation())
	end, endErr := time.ParseInLocation("2006-01-02", m.period.MeasuredUntil, clock.HouseholdLocation())
	if startErr != nil || endErr != nil {
		return nil
	}
	series := []string{}
	total := "0"
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		if v, ok := m.dailyNet[d.Format("2006-01-02")]; ok {
			total = financialmath.Add(total, v)
		}
		series = append(series, total)
	}
	return series
}

func buildPace(previousFull *cycleMeasure, eligible []cycleMeasure, medianAvailable bool) paceBaselines {
	var pace paceBaselines
	if previousFull != nil {
		pace.PreviousFullCycle = cumulativeSeries(*previousFull)
	}
	if medianAvailable && len(eligible) == 3 {
		series := [][]string{cumulativeSeries(eligible[0]), cumulativeSeries(eligible[1]), cumulativeSeries(eligible[2])}
		shared := min(len(series[0]), len(series[1]), len(series[2]))
		for i := 0; i < shared; i++ {
			pace.Median3 = append(pace.Median3, *median3([]string{series[0][i], series[1][i], series[2][i]}))
		}
	}
	return pace
}

// historyWindow selects the ledger cycles from periods (newest first): the n most
// recent, or n ending at the selected cycle when it is older, returned oldest first.
// Closed cycles are measured to their full exclusive end.
func historyWindow(periods []reviewPeriod, index, n int) []reviewPeriod {
	if n <= 0 || index < 0 || index >= len(periods) {
		return nil
	}
	from := 0
	if index >= n {
		from = index
	}
	to := from + n
	if to > len(periods) {
		to = len(periods)
	}
	window := make([]reviewPeriod, 0, to-from)
	for i := to - 1; i >= from; i-- {
		p := periods[i]
		if p.State == "CLOSED" && p.End != nil {
			p.MeasuredUntil = *p.End
		}
		window = append(window, p)
	}
	return window
}

// historyMeasures reuses already-loaded measures with the same start and
// cutoff, and appends the rest. It returns the window as measure indices.
func historyMeasures(measures []cycleMeasure, window []reviewPeriod) ([]cycleMeasure, []int) {
	seen := map[string]int{}
	for i, m := range measures {
		seen[m.period.Start+"|"+m.period.MeasuredUntil] = i
	}
	indices := make([]int, 0, len(window))
	for _, p := range window {
		key := p.Start + "|" + p.MeasuredUntil
		i, ok := seen[key]
		if !ok {
			measures = append(measures, newCycleMeasure(p))
			i = len(measures) - 1
			seen[key] = i
		}
		indices = append(indices, i)
	}
	return measures, indices
}

// sameDayWindow cuts each window cycle at the selected active cycle's elapsed
// day count (a cycle shorter than that is measured to its own end). The active
// cycle itself keeps its measured cutoff.
func sameDayWindow(window []reviewPeriod, elapsed int) []reviewPeriod {
	out := make([]reviewPeriod, 0, len(window))
	for _, p := range window {
		out = append(out, sameDayPeriod(p, elapsed))
	}
	return out
}

// sameDayPeriod cuts a closed cycle at the first `elapsed` calendar days
// (start-inclusive, exclusive end on the Asia/Jakarta date grid). A cycle no
// longer than that is measured to its own end; open cycles are untouched.
func sameDayPeriod(p reviewPeriod, elapsed int) reviewPeriod {
	if p.State != "CLOSED" || p.End == nil {
		return p
	}
	start, err := time.ParseInLocation("2006-01-02", p.Start, clock.HouseholdLocation())
	if err != nil {
		return p
	}
	if cut := start.AddDate(0, 0, elapsed).Format("2006-01-02"); cut < *p.End {
		p.MeasuredUntil = cut
	} else {
		p.MeasuredUntil = *p.End
	}
	return p
}

// buildHistory turns loaded window measures (oldest first) into the ledger
// series. Amounts are Go-computed whole-IDR strings; "Other" is net expense minus
// the listed rows, so each cycle reconciles exactly. sameDay holds the indices of
// the same cycles cut at one elapsed day (nil unless the selected cycle is
// ACTIVE); its rows are the full matrix's rows. selectedStart picks the column
// that deltaVsPrevious is measured for.
func buildHistory(measures []cycleMeasure, window, sameDay []int, selectedStart string) ([]historyCycle, categoryHistory) {
	history := make([]historyCycle, 0, len(window))
	totals, names := map[string]string{}, map[string]string{}
	selected := -1
	for pos, i := range window {
		m := measures[i]
		if m.period.Start == selectedStart {
			selected = pos
		}
		history = append(history, historyCycle{Start: m.period.Start, End: m.period.End, MeasuredUntil: m.period.MeasuredUntil, State: m.period.State,
			Income: m.cash.Income, GrossExpense: m.cash.GrossExpense, Refund: m.cash.Refund, Expense: m.cash.Expense, Net: m.cash.Net, SavingsAllocated: m.cash.Allocated})
		for id, v := range m.categories {
			if _, ok := totals[id]; !ok {
				totals[id], names[id] = "0", v.Name
			}
			totals[id] = financialmath.Add(totals[id], v.Amount)
		}
	}
	for i := 1; i < len(history); i++ {
		history[i].ExpenseDelta = valuePointer(financialmath.Subtract(history[i].Expense, history[i-1].Expense))
	}
	ids := make([]string, 0, len(totals))
	for id, total := range totals {
		if compareAmounts(total, "0") != 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool {
		if c := compareAmounts(totals[ids[a]], totals[ids[b]]); c != 0 {
			return c > 0
		}
		if names[ids[a]] != names[ids[b]] {
			return names[ids[a]] < names[ids[b]]
		}
		return ids[a] < ids[b]
	})
	if len(ids) > rankingTopCategories {
		ids = ids[:rankingTopCategories]
	}
	matrix := categoryHistory{categoryMatrix: buildMatrix(measures, window, ids, names, selected)}
	if len(sameDay) == len(window) && len(window) > 0 && selected >= 0 {
		cut := measures[sameDay[selected]].period
		days := make([]int, 0, len(sameDay))
		for _, i := range sameDay {
			days = append(days, daysBetween(measures[i].period.Start, measures[i].period.MeasuredUntil))
		}
		matrix.SameDay = &sameDayMatrix{ElapsedDays: daysBetween(cut.Start, cut.MeasuredUntil), MeasuredDays: days, categoryMatrix: buildMatrix(measures, sameDay, ids, names, selected)}
	}
	return history, matrix
}

// buildMatrix lays out the listed category ids by cycle. Other = net expense minus
// the listed rows and Totals = net expense, so rows + Other = Totals per column.
// With a selected column after the first, every row, Other and the total carry
// selected minus the column before it.
func buildMatrix(measures []cycleMeasure, idxs []int, ids []string, names map[string]string, selected int) categoryMatrix {
	matrix := emptyMatrix()
	for _, id := range ids {
		matrix.Rows = append(matrix.Rows, historyCategory{ID: id, Name: names[id], Amounts: make([]string, 0, len(idxs))})
	}
	for _, i := range idxs {
		m := measures[i]
		matrix.CycleStarts = append(matrix.CycleStarts, m.period.Start)
		listed := "0"
		for r, id := range ids {
			amount := "0"
			if v, ok := m.categories[id]; ok {
				amount = v.Amount
			}
			matrix.Rows[r].Amounts = append(matrix.Rows[r].Amounts, amount)
			listed = financialmath.Add(listed, amount)
		}
		matrix.Other.Amounts = append(matrix.Other.Amounts, financialmath.Subtract(m.cash.Expense, listed))
		matrix.Totals = append(matrix.Totals, m.cash.Expense)
	}
	if selected >= 0 && selected < len(idxs) {
		matrix.SelectedIndex = &selected
	}
	if selected > 0 && selected < len(idxs) {
		delta := func(a []string) *string { return valuePointer(financialmath.Subtract(a[selected], a[selected-1])) }
		for r := range matrix.Rows {
			matrix.Rows[r].DeltaVsPrevious = delta(matrix.Rows[r].Amounts)
		}
		matrix.Other.DeltaVsPrevious = delta(matrix.Other.Amounts)
		matrix.TotalDeltaVsPrevious = delta(matrix.Totals)
	}
	return matrix
}

func emptyMerchantRanking() merchantRanking {
	return merchantRanking{Rows: []merchantRankRow{}, Other: merchantRankOther{Amount: "0"}, Total: merchantRankTotal{Amount: "0"}}
}

// noMerchantName labels expense rows with neither a merchant nor a counterparty.
const noMerchantName = "Tanpa merchant"

func merchantKind(v reviewValue) string {
	switch {
	case v.ID != "":
		return "MERCHANT"
	case v.Name == noMerchantName:
		return "NONE"
	}
	return "COUNTERPARTY"
}

// buildMerchantRanking ranks the selected cycle's payees by net amount (largest
// first, numerically) and compares them with measures[1], the comparison cycle.
// Payees with a zero or negative net, and everything after the top rows, fall
// into Other, which is computed as total minus listed so rows + Other = Total =
// the cycle's net expense to the rupiah.
func buildMerchantRanking(measures []cycleMeasure, mode string) merchantRanking {
	ranking := emptyMerchantRanking()
	ranking.Mode = mode
	current := measures[0]
	var previous *cycleMeasure
	if len(measures) > 1 {
		previous = &measures[1]
	}
	type entry struct {
		key string
		v   reviewValue
	}
	entries := make([]entry, 0, len(current.merchants))
	totalCount := 0
	for key, v := range current.merchants {
		entries = append(entries, entry{key, v})
		totalCount += v.Count
	}
	sort.Slice(entries, func(i, j int) bool {
		if c := compareAmounts(entries[i].v.Amount, entries[j].v.Amount); c != 0 {
			return c > 0
		}
		if entries[i].v.Name != entries[j].v.Name {
			return entries[i].v.Name < entries[j].v.Name
		}
		return entries[i].key < entries[j].key
	})
	ranking.Total = merchantRankTotal{Amount: current.cash.Expense, Count: totalCount}
	if previous != nil {
		ranking.Total.Previous = valuePointer(previous.cash.Expense)
		ranking.Total.Delta = valuePointer(financialmath.Subtract(current.cash.Expense, previous.cash.Expense))
	}
	listed, listedPrevious, listedCount := "0", "0", 0
	for _, e := range entries {
		if len(ranking.Rows) == merchantRankingRows || compareAmounts(e.v.Amount, "0") <= 0 {
			break
		}
		row := merchantRankRow{reviewValue: e.v, Kind: merchantKind(e.v), Share: positiveRatio(e.v.Amount, current.cash.Expense)}
		if previous != nil {
			prior := "0"
			if v, ok := previous.merchants[e.key]; ok {
				prior = v.Amount
			}
			row.Previous = valuePointer(prior)
			row.Delta = valuePointer(financialmath.Subtract(e.v.Amount, prior))
			listedPrevious = financialmath.Add(listedPrevious, prior)
		}
		ranking.Rows = append(ranking.Rows, row)
		listed = financialmath.Add(listed, e.v.Amount)
		listedCount += e.v.Count
	}
	ranking.Other = merchantRankOther{Amount: financialmath.Subtract(current.cash.Expense, listed), Count: totalCount - listedCount, MerchantCount: len(entries) - len(ranking.Rows)}
	if previous != nil {
		ranking.Other.Previous = valuePointer(financialmath.Subtract(previous.cash.Expense, listedPrevious))
		ranking.Other.Delta = valuePointer(financialmath.Subtract(ranking.Other.Amount, *ranking.Other.Previous))
	}
	return ranking
}

func (f *Facts) block(kind string, count int, amount *string, action string) {
	if count > 0 {
		scope, impact := "CASHFLOW", "CASHFLOW_PARTIAL"
		switch kind {
		case "PROCESSING_INCOMPLETE":
			scope, impact = "PROCESSING", "PROCESSING_INCOMPLETE"
		case "MISSING_WEALTH_SNAPSHOT", "WEALTH_SNAPSHOT_BEFORE_CYCLE", "MISSING_PREVIOUS_WEALTH_SNAPSHOT", "WEALTH_ACCOUNT_SET_CHANGED":
			scope, impact = "WEALTH_RECONCILIATION", "COMPARISON_UNAVAILABLE"
		}
		f.Quality = append(f.Quality, reviewBlocker{Kind: kind, Count: count, Amount: amount, Scope: scope, Impact: impact, Action: action})
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
		c.Delta = valuePointer(financialmath.Subtract(current, *previous))
		c.Relative = positiveRatio(*c.Delta, *previous)
	}
	if median != nil {
		c.DeltaMedian = valuePointer(financialmath.Subtract(current, *median))
		c.RelativeMedian = positiveRatio(*c.DeltaMedian, *median)
	}
	return c
}

func positiveRatio(n, d string) *string {
	if v, ok := financialmath.Ratio(n, d); ok {
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

func cashChange(measures []cycleMeasure, amount func(reviewCashflow) string, full *cycleMeasure) reviewChange {
	var previous *string
	history := []string{}
	for _, m := range measures[1:] {
		history = append(history, amount(m.cash))
	}
	if len(history) > 0 {
		previous = &history[0]
	}
	c := change(amount(measures[0].cash), previous, median3(history))
	if full != nil {
		c.setFullPrevious(amount(full.cash))
	}
	return c
}

func (c *reviewChange) setFullPrevious(amount string) {
	c.PreviousFull = valuePointer(amount)
	c.DeltaFull = valuePointer(financialmath.Subtract(c.Amount, amount))
	c.RelativeFull = positiveRatio(*c.DeltaFull, amount)
}

func changes(measures []cycleMeasure, values func(cycleMeasure) map[string]reviewValue, full *cycleMeasure) []reviewChange {
	names := map[string]reviewValue{}
	if full != nil {
		for id, v := range values(*full) {
			names[id] = v
		}
	}
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
		if full != nil {
			amount := "0"
			if v, ok := values(*full)[id]; ok {
				amount = v.Amount
			}
			c.setFullPrevious(amount)
		}
		c.Share = positiveRatio(current.Amount, measures[0].cash.Expense)
		if c.Delta != nil {
			den, _ := new(big.Int).SetString(financialmath.Subtract(measures[0].cash.Expense, measures[1].cash.Expense), 10)
			if den.Sign() != 0 {
				num, _ := new(big.Int).SetString(*c.Delta, 10)
				c.Contribution = valuePointer(new(big.Rat).SetFrac(num, den).FloatString(4))
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Amount, out[j].Amount
		if out[i].DeltaFull != nil {
			a, b = *out[i].DeltaFull, *out[j].DeltaFull
		} else if out[i].Delta != nil {
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

// orderedMembers keeps attribution descriptive, never a ranking: the automatic
// bucket first, then members by name. shareOfExpense is Go-computed in
// basis points with largest-remainder rounding, so the shares sum to exactly
// 1.0000; it is null when net expense is not positive or a member is negative.
func orderedMembers(values map[string]reviewValue) []reviewMember {
	out := make([]reviewMember, 0, len(values))
	total := big.NewInt(0)
	shares := true
	for _, v := range values {
		kind := "MEMBER"
		if v.ID == "" {
			kind = "AUTOMATIC"
		}
		out = append(out, reviewMember{reviewValue: v, Kind: kind})
		amount, _ := new(big.Int).SetString(v.Amount, 10)
		if amount.Sign() < 0 {
			shares = false
		}
		total.Add(total, amount)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Kind == "AUTOMATIC") != (out[j].Kind == "AUTOMATIC") {
			return out[i].Kind == "AUTOMATIC"
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	if !shares || total.Sign() <= 0 {
		return out
	}
	const scale = 10000
	basis, remainders := make([]int64, len(out)), make([]*big.Int, len(out))
	assigned := int64(0)
	for i, m := range out {
		amount, _ := new(big.Int).SetString(m.Amount, 10)
		quotient, remainder := new(big.Int).QuoRem(new(big.Int).Mul(amount, big.NewInt(scale)), total, new(big.Int))
		basis[i], remainders[i] = quotient.Int64(), remainder
		assigned += basis[i]
	}
	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return remainders[order[a]].Cmp(remainders[order[b]]) > 0 })
	for k := int64(0); k < scale-assigned; k++ {
		basis[order[k]]++
	}
	for i := range out {
		out[i].Share = valuePointer(fmt.Sprintf("%d.%04d", basis[i]/scale, basis[i]%scale))
	}
	return out
}

func memberValues(members []reviewMember) []reviewValue {
	out := make([]reviewValue, 0, len(members))
	for _, m := range members {
		out = append(out, m.reviewValue)
	}
	return out
}
