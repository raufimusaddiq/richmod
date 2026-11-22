package financialmath

import (
	"fmt"
	"math/big"
)

type Cashflow struct {
	Income     string
	Expense    string
	Refund     string
	NetExpense string
	Surplus    string
}

func parseDecimalInt(raw string) (*big.Int, bool) {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return big.NewInt(0), false
	}
	return value, true
}

func Add(a, b string) string {
	x, _ := parseDecimalInt(a)
	y, _ := parseDecimalInt(b)
	return new(big.Int).Add(x, y).String()
}

func Subtract(a, b string) string {
	x, _ := parseDecimalInt(a)
	y, _ := parseDecimalInt(b)
	return new(big.Int).Sub(x, y).String()
}

func Ratio(n, d string) (string, bool) {
	num, numOK := parseDecimalInt(n)
	den, denOK := parseDecimalInt(d)
	if !numOK || !denOK || den.Sign() <= 0 {
		return "", false
	}
	return new(big.Rat).SetFrac(num, den).FloatString(4), true
}

func CalculateCashflow(income, expense, refund string) (Cashflow, error) {
	values := make([]*big.Int, 3)
	for i, raw := range []string{income, expense, refund} {
		value, ok := new(big.Int).SetString(raw, 10)
		if !ok {
			return Cashflow{}, fmt.Errorf("invalid cashflow amount")
		}
		values[i] = value
	}
	netExpense := new(big.Int).Sub(values[1], values[2])
	surplus := new(big.Int).Sub(values[0], netExpense)
	return Cashflow{Income: income, Expense: expense, Refund: refund, NetExpense: netExpense.String(), Surplus: surplus.String()}, nil
}
