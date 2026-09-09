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
