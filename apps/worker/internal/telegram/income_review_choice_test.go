package telegram

import "testing"

func TestIncomeReviewChoiceAcceptsOnlyTheBoundedAnswers(t *testing.T) {
	for text, want := range map[string]string{
		"penghasilan":      "CONFIRM",
		" Penghasilan! ":   "CONFIRM",
		"pemasukan":        "CONFIRM",
		"transfer sendiri": "REJECT",
		"Transfer.":        "REJECT",
		// Negations, questions and longer sentences must never change state.
		"ini tidak termasuk penghasilan":     "",
		"ini bukan transfer, ini gaji":       "",
		"penghasilan atau transfer sendiri?": "",
		"bukan penghasilan":                  "",
		"mungkin transfer sendiri deh":       "",
		"":                                   "",
	} {
		if got := incomeReviewChoice(text); got != want {
			t.Errorf("incomeReviewChoice(%q)=%q, want %q", text, got, want)
		}
	}
}
