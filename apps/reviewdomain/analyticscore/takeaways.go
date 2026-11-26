package analyticscore

import (
	"fmt"
	"math/big"
	"strings"
)

const takeawayHistoryCycles = 3

type reviewTakeaways struct {
	Where   string `json:"where,omitempty"`
	History string `json:"history,omitempty"`
	Changes string `json:"changes,omitempty"`
	Pace    string `json:"pace,omitempty"`
	Verdict string `json:"verdict,omitempty"`
}

func buildTakeaways(f Facts) reviewTakeaways {
	return reviewTakeaways{
		Where:   whereTakeaway(f),
		History: historyTakeaway(f),
		Changes: changesTakeaway(f),
		Pace:    paceTakeaway(f),
		Verdict: verdictTakeaway(f),
	}
}

func parseAmount(value string) (*big.Int, bool) {
	return new(big.Int).SetString(value, 10)
}

func percentText(part, whole *big.Int) string {
	rat := new(big.Rat).SetFrac(new(big.Int).Mul(part, big.NewInt(100)), whole)
	rounded := new(big.Int).Add(new(big.Int).Mul(rat.Num(), big.NewInt(2)), rat.Denom())
	rounded.Div(rounded, new(big.Int).Mul(rat.Denom(), big.NewInt(2)))
	return rounded.String() + "%"
}

func millionsText(value string) string {
	amount, ok := parseAmount(value)
	if !ok {
		return ""
	}
	sign, abs := "", new(big.Int).Abs(amount)
	if amount.Sign() < 0 {
		sign = "-"
	}
	million := big.NewInt(1000000)
	if abs.Cmp(million) < 0 {
		return fmt.Sprintf("%sRp %s", sign, thousandsText(abs.String()))
	}
	hundredths := new(big.Int).Add(abs, big.NewInt(5000))
	hundredths.Div(hundredths, big.NewInt(10000))
	whole, fraction := new(big.Int).DivMod(hundredths, big.NewInt(100), new(big.Int))
	return fmt.Sprintf("%sRp %s,%02d jt", sign, thousandsText(whole.String()), fraction.Int64())
}

func thousandsText(digits string) string {
	var out []string
	for len(digits) > 3 {
		out = append([]string{digits[len(digits)-3:]}, out...)
		digits = digits[:len(digits)-3]
	}
	return strings.Join(append([]string{digits}, out...), ".")
}

func joinNames(names []string) string {
	return strings.Join(names, " dan ")
}

func whereTakeaway(f Facts) string {
	whole, ok := parseAmount(f.Cashflow.Expense)
	if !ok || whole.Sign() <= 0 {
		return ""
	}
	rows := f.CategoryRanking.Rows
	if len(rows) == 0 {
		return ""
	}
	top, ok := parseAmount(rows[0].Amount)
	if !ok || top.Sign() <= 0 {
		return ""
	}
	named := rows[:1]
	if len(rows) > 1 {
		second, ok := parseAmount(rows[1].Amount)
		if !ok || second.Sign() < 0 {
			return ""
		}
		dominant := new(big.Int).Mul(top, big.NewInt(2)).Cmp(whole) >= 0
		negligible := new(big.Int).Mul(second, big.NewInt(10)).Cmp(whole) < 0
		if !dominant && !negligible {
			named = rows[:2]
		}
	}
	rows = named
	sum, names := new(big.Int), make([]string, 0, len(rows))
	for _, row := range rows {
		amount, ok := parseAmount(row.Amount)
		if !ok || amount.Sign() <= 0 {
			return ""
		}
		sum.Add(sum, amount)
		names = append(names, row.Name)
	}
	return fmt.Sprintf("%s menghabiskan %s pengeluaran siklus ini.", joinNames(names), percentText(sum, whole))
}

func historyTakeaway(f Facts) string {
	selected := -1
	for i, cycle := range f.History {
		if cycle.Start == f.Period.Start {
			selected = i
		}
	}
	if selected < 1 {
		return ""
	}
	from := selected - takeawayHistoryCycles + 1
	if from < 0 {
		from = 0
	}
	parts := make([]string, 0, takeawayHistoryCycles)
	for _, cycle := range f.History[from : selected+1] {
		text := millionsText(cycle.Expense)
		if text == "" {
			return ""
		}
		parts = append(parts, text)
	}
	last := f.History[selected]
	if last.State == "ACTIVE" {
		parts[len(parts)-1] += fmt.Sprintf(" (berjalan, hari ke-%d)", daysBetween(last.Start, last.MeasuredUntil))
	} else {
		parts[len(parts)-1] += " (ditutup)"
	}
	text := strings.Join(parts, " → ")
	if last.State == "CLOSED" && selected >= 1 {
		if sentence := perDayChange(last, f.History[selected-1]); sentence != "" {
			text += " " + sentence
		}
	}
	return text
}

func cycleDays(c historyCycle) int {
	if c.Days > 0 {
		return c.Days
	}
	return daysBetween(c.Start, c.MeasuredUntil)
}

func cycleAverage(c historyCycle) string {
	if c.AverageDailyExpenseRounded != "" {
		return c.AverageDailyExpenseRounded
	}
	return roundedPerDay(c.Expense, cycleDays(c))
}

func percentTenthsText(part, whole *big.Int) string {
	rat := new(big.Rat).SetFrac(new(big.Int).Mul(part, big.NewInt(1000)), whole)
	rounded := new(big.Int).Add(new(big.Int).Mul(rat.Num(), big.NewInt(2)), rat.Denom())
	rounded.Div(rounded, new(big.Int).Mul(rat.Denom(), big.NewInt(2)))
	whole10, fraction := new(big.Int).DivMod(rounded, big.NewInt(10), new(big.Int))
	return fmt.Sprintf("%s,%d%%", thousandsText(whole10.String()), fraction.Int64())
}

func perDayChange(current, previous historyCycle) string {
	if current.State != "CLOSED" || previous.State != "CLOSED" || cycleDays(current) == cycleDays(previous) {
		return ""
	}
	currentAverage, previousAverage := cycleAverage(current), cycleAverage(previous)
	cur, curOK := parseAmount(currentAverage)
	prev, prevOK := parseAmount(previousAverage)
	if !curOK || !prevOK || prev.Sign() <= 0 {
		return ""
	}
	head := fmt.Sprintf("Per hari Rp %s vs Rp %s, ", thousandsText(currentAverage), thousandsText(previousAverage))
	diff := new(big.Int).Sub(cur, prev)
	if diff.Sign() == 0 {
		return head + "sama."
	}
	direction := "tinggi"
	if diff.Sign() < 0 {
		direction = "rendah"
	}
	return head + fmt.Sprintf("%s lebih %s.", percentTenthsText(new(big.Int).Abs(diff), prev), direction)
}

func categoryDelta(f Facts, c reviewCategory) *string {
	return shownDelta(f.Period.State, c.Delta, c.DeltaFull)
}

func verdictTakeaway(f Facts) string {
	var delta *string
	suffix := " (penuh)"
	if f.Period.State == "ACTIVE" {
		delta = f.Comparison.Expense.Delta
		suffix = fmt.Sprintf(" di hari ke-%d", daysBetween(f.Period.Start, f.Period.MeasuredUntil))
	} else {
		delta = f.Comparison.Expense.DeltaFull
	}
	if delta == nil {
		return ""
	}
	amount, ok := parseAmount(*delta)
	if !ok {
		return ""
	}
	if amount.Sign() == 0 {
		return fmt.Sprintf("Pengeluaran sama dengan siklus lalu%s.", suffix)
	}
	direction := "lebih tinggi"
	if amount.Sign() < 0 {
		direction = "lebih rendah"
	}
	return fmt.Sprintf("Rp %s %s dari siklus lalu%s.", thousandsText(new(big.Int).Abs(amount).String()), direction, suffix)
}

func changesTakeaway(f Facts) string {
	if f.Comparison.Previous == nil && f.Comparison.PreviousFullCycle == nil {
		return ""
	}
	var up, down *reviewCategory
	var upDelta, downDelta *big.Int
	for i := range f.Categories {
		raw := categoryDelta(f, f.Categories[i])
		if raw == nil {
			continue
		}
		delta, ok := parseAmount(*raw)
		if !ok {
			continue
		}
		switch {
		case delta.Sign() > 0 && (upDelta == nil || delta.Cmp(upDelta) > 0):
			up, upDelta = &f.Categories[i], delta
		case delta.Sign() < 0 && (downDelta == nil || delta.Cmp(downDelta) < 0):
			down, downDelta = &f.Categories[i], delta
		}
	}
	switch {
	case up != nil && down != nil:
		return fmt.Sprintf("Naik paling banyak %s, turun paling banyak %s.", up.Name, down.Name)
	case up != nil:
		return fmt.Sprintf("Naik paling banyak %s.", up.Name)
	case down != nil:
		return fmt.Sprintf("Turun paling banyak %s.", down.Name)
	}
	return ""
}

func paceTakeaway(f Facts) string {
	if len(f.Daily) == 0 || f.Comparison.Previous == nil || f.Comparison.Expense.Previous == nil {
		return ""
	}
	current := millionsText(f.Daily[len(f.Daily)-1]["cumulativeExpense"])
	previous := millionsText(*f.Comparison.Expense.Previous)
	if current == "" || previous == "" {
		return ""
	}
	previousDays := daysBetween(f.Comparison.Previous.Start, f.Comparison.Previous.MeasuredUntil)
	days := f.SpendingShape.Days
	if f.Period.State == "ACTIVE" {
		return fmt.Sprintf("Hari ke-%d: %s · siklus lalu %s (%d hari).", days, current, previous, previousDays)
	}
	return fmt.Sprintf("Siklus ini %s (%d hari) · siklus lalu %s (%d hari).", current, days, previous, previousDays)
}
