package analyticscore

import (
	"math/big"
	"sort"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
)

const (
	rankingTopCategories = 7
	// RankingOtherName is the display name of the remainder bucket.
	RankingOtherName = "Kategori lain"
)

// rankedCategory is one amount-ranked category row. ID is null for spending
// without a category (the explicit "Belum dikategorikan" row).
type rankedCategory struct {
	ID       *string `json:"id"`
	Name     string  `json:"name"`
	Amount   string  `json:"amount"`
	Count    int     `json:"count"`
	Share    *string `json:"shareOfExpense"`
	Baseline *string `json:"baselineAmount"`
	Delta    *string `json:"deltaVsBaseline"`
	Relative *string `json:"relativeDeltaVsBaseline"`
}

type rankedOther struct {
	Name          string  `json:"name"`
	Amount        string  `json:"amount"`
	Count         int     `json:"count"`
	CategoryCount int     `json:"categoryCount"`
	Share         *string `json:"shareOfExpense"`
	Baseline      *string `json:"baselineAmount"`
	Delta         *string `json:"deltaVsBaseline"`
	Relative      *string `json:"relativeDeltaVsBaseline"`
}

// categoryRanking is the "where did the money go" list: the top categories by
// net expense (numeric, descending) plus one remainder bucket, so rows plus
// other equal cashflow.expense exactly. Baseline is SAME_DAY (previous cycle at
// the same elapsed day) while the cycle is active, PREVIOUS_FULL otherwise; the
// baseline fields are null when there is nothing to compare against.
type categoryRanking struct {
	Baseline string           `json:"baseline"`
	Rows     []rankedCategory `json:"rows"`
	Other    rankedOther      `json:"other"`
}

func emptyCategoryRanking() categoryRanking {
	return categoryRanking{Baseline: "PREVIOUS_FULL", Rows: []rankedCategory{}, Other: rankedOther{Name: RankingOtherName, Amount: "0"}}
}

// buildCategoryRanking ranks current's categories. baseline is the comparison
// measure (nil when absent) and mode the comparison mode.
func buildCategoryRanking(current cycleMeasure, baseline *cycleMeasure, mode string) categoryRanking {
	out := emptyCategoryRanking()
	if mode == "ELAPSED_DAYS" {
		out.Baseline = "SAME_DAY"
	}
	expense := current.cash.Expense
	candidates := make([]reviewValue, 0, len(current.categories))
	for _, v := range current.categories {
		if compareAmounts(v.Amount, "0") != 0 {
			candidates = append(candidates, v)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if c := compareAmounts(candidates[i].Amount, candidates[j].Amount); c != 0 {
			return c > 0
		}
		if candidates[i].Name != candidates[j].Name {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].ID < candidates[j].ID
	})
	top := candidates
	if len(top) > rankingTopCategories {
		top = top[:rankingTopCategories]
	}
	listed, listedBase := "0", "0"
	for _, v := range top {
		row := rankedCategory{Name: v.Name, Amount: v.Amount, Count: v.Count, Share: positiveRatio(v.Amount, expense)}
		if v.ID != "" {
			id := v.ID
			row.ID = &id
		}
		if baseline != nil {
			b := "0"
			if bv, ok := baseline.categories[v.ID]; ok {
				b = bv.Amount
			}
			row.Baseline = &b
			row.Delta, row.Relative = deltaPair(v.Amount, b)
			listedBase = financialmath.Add(listedBase, b)
		}
		listed = financialmath.Add(listed, v.Amount)
		out.Rows = append(out.Rows, row)
	}
	out.Other.Amount = financialmath.Subtract(expense, listed)
	out.Other.Share = positiveRatio(out.Other.Amount, expense)
	for _, v := range candidates[len(top):] {
		out.Other.Count += v.Count
		out.Other.CategoryCount++
	}
	if baseline != nil {
		b := financialmath.Subtract(baseline.cash.Expense, listedBase)
		out.Other.Baseline = &b
		out.Other.Delta, out.Other.Relative = deltaPair(out.Other.Amount, b)
	}
	return out
}

func deltaPair(current, baseline string) (*string, *string) {
	d := financialmath.Subtract(current, baseline)
	return &d, positiveRatio(d, baseline)
}

// roundedPerDay is amount/days rounded half away from zero to whole rupiah.
func roundedPerDay(amount string, days int) string {
	n, ok := new(big.Int).SetString(amount, 10)
	if !ok || days <= 0 {
		return "0"
	}
	q := new(big.Rat).SetFrac(n, big.NewInt(int64(days)))
	neg := q.Sign() < 0
	q.Abs(q)
	q.Add(q, big.NewRat(1, 2))
	r := new(big.Int).Quo(q.Num(), q.Denom())
	if neg && r.Sign() != 0 {
		r.Neg(r)
	}
	return r.String()
}
