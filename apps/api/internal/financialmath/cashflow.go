package financialmath

import shared "github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"

type Cashflow = shared.Cashflow

func CalculateCashflow(income, expense, refund string) (Cashflow, error) {
	return shared.CalculateCashflow(income, expense, refund)
}

func Add(a, b string) string           { return shared.Add(a, b) }
func Subtract(a, b string) string      { return shared.Subtract(a, b) }
func Ratio(n, d string) (string, bool) { return shared.Ratio(n, d) }
