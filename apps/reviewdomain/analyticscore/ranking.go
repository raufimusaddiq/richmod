package analyticscore

import (
	"math/big"
	"sort"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
)

const (
	rankingTopCategories = 6
	RankingOtherName     = "Kategori lain"
)

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
	Name          string           `json:"name"`
	Amount        string           `json:"amount"`
	Count         int              `json:"count"`
	CategoryCount int              `json:"categoryCount"`
	Share         *string          `json:"shareOfExpense"`
	Baseline      *string          `json:"baselineAmount"`
	Delta         *string          `json:"deltaVsBaseline"`
	Relative      *string          `json:"relativeDeltaVsBaseline"`
	Rows          []rankedCategory `json:"rows"`
}

type categoryRanking struct {
	Baseline string           `json:"baseline"`
	Rows     []rankedCategory `json:"rows"`
	Other    rankedOther      `json:"other"`
}

func emptyCategoryRanking() categoryRanking {
	return categoryRanking{
		Baseline: "PREVIOUS_FULL",
		Rows:     []rankedCategory{},
		Other:    rankedOther{Name: RankingOtherName, Amount: "0", Rows: []rankedCategory{}},
	}
}

func buildRankedRow(v reviewValue, expense string, baseline *cycleMeasure) (rankedCategory, string) {
	row := rankedCategory{Name: v.Name, Amount: v.Amount, Count: v.Count, Share: positiveRatio(v.Amount, expense)}
	if v.ID != "" {
		id := v.ID
		row.ID = &id
	}
	baseAmount := "0"
	if baseline != nil {
		if bv, ok := baseline.categories[v.ID]; ok {
			baseAmount = bv.Amount
		}
		b := baseAmount
		row.Baseline = &b
		row.Delta, row.Relative = deltaPair(v.Amount, b)
	}
	return row, baseAmount
}

func buildCategoryRanking(current cycleMeasure, baseline *cycleMeasure, mode string, topIDs []string) categoryRanking {
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
	topSet := map[string]bool{}
	for _, id := range topIDs {
		if id == "" {
			continue
		}
		if len(topSet) >= rankingTopCategories {
			break
		}
		topSet[id] = true
	}
	top := make([]reviewValue, 0, rankingTopCategories)
	rest := make([]reviewValue, 0, len(candidates))
	for _, v := range candidates {
		positiveCategorized := v.ID != "" && compareAmounts(v.Amount, "0") > 0
		if len(topSet) > 0 {
			if positiveCategorized && topSet[v.ID] {
				top = append(top, v)
			} else {
				rest = append(rest, v)
			}
			continue
		}
		if positiveCategorized && len(top) < rankingTopCategories {
			top = append(top, v)
		} else {
			rest = append(rest, v)
		}
	}
	listed, listedBase := "0", "0"
	for _, v := range top {
		row, baseAmount := buildRankedRow(v, expense, baseline)
		if baseline != nil {
			listedBase = financialmath.Add(listedBase, baseAmount)
		}
		listed = financialmath.Add(listed, v.Amount)
		out.Rows = append(out.Rows, row)
	}
	out.Other.Amount = financialmath.Subtract(expense, listed)
	out.Other.Share = positiveRatio(out.Other.Amount, expense)
	for _, v := range rest {
		row, _ := buildRankedRow(v, expense, baseline)
		out.Other.Rows = append(out.Other.Rows, row)
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
