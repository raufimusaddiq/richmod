package review

import "testing"

// PRD §13.4: the Inbox must never render a required input for a fact the review
// already holds. These helpers decide which fields a card may request.
func TestMissingFieldNamesTheUnresolvedDimension(t *testing.T) {
	cases := []struct {
		missing []string
		field   string
		want    bool
	}{
		{[]string{"category"}, "category", true},
		{[]string{"category"}, "merchant", false},
		{[]string{"merchant"}, "merchant", true},
		{nil, "category", false},
	}
	for _, testCase := range cases {
		if got := missingField(testCase.missing, testCase.field); got != testCase.want {
			t.Fatalf("missingField(%v,%q)=%v want %v", testCase.missing, testCase.field, got, testCase.want)
		}
	}
}

