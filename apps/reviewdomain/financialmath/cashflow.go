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

// Add, Subtract and Ratio consume validated integer IDR amounts from PostgreSQL.
func Add(a, b string) string {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return new(big.Int).Add(x, y).String()
}

func Subtract(a, b string) string {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return new(big.Int).Sub(x, y).String()
}

func Ratio(n, d string) (string, bool) {
	num, _ := new(big.Int).SetString(n, 10)
	den, _ := new(big.Int).SetString(d, 10)
	if den.Sign() <= 0 {
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
