package analyticscore

import (
	"testing"
	"time"
)

func TestPickSnapshots(t *testing.T) {
	at := time.Date(2026, 10, 22, 14, 53, 0, 0, time.UTC)
	cur := &reviewSnapshot{ID: "b", At: at}
	prev := &reviewSnapshot{ID: "a", At: at.AddDate(0, 0, -13)}
	same := &reviewSnapshot{ID: "c", At: at} // same timestamp, later id in the total order

	for _, tc := range []struct {
		name     string
		in       []*reviewSnapshot
		wantCur  *reviewSnapshot
		wantPrev *reviewSnapshot
	}{
		{"predecessor exists", []*reviewSnapshot{cur, prev}, cur, prev},
		{"no predecessor", []*reviewSnapshot{cur}, cur, nil},
		{"none at all", nil, nil, nil},
		{"same timestamp uses id order", []*reviewSnapshot{cur, same}, cur, same},
		{"never the same snapshot twice", []*reviewSnapshot{cur, cur}, cur, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, p := pickSnapshots(tc.in)
			if c != tc.wantCur || p != tc.wantPrev {
				t.Fatalf("got %+v/%+v", c, p)
			}
			if c != nil && p != nil && c.ID == p.ID {
				t.Fatal("previous equals current")
			}
		})
	}
}
