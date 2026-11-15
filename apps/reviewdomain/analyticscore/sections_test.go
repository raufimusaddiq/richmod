package analyticscore

import (
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
)

func TestSameDayWindowCutsEveryClosedCycleAtTheSameElapsedDay(t *testing.T) {
	end1, end2 := "2026-07-01", "2026-07-20"
	window := []reviewPeriod{
		{Start: "2026-06-01", End: &end1, MeasuredUntil: end1, State: "CLOSED"},
		{Start: "2026-07-01", End: &end2, MeasuredUntil: end2, State: "CLOSED"},
		{Start: "2026-07-20", MeasuredUntil: "2026-08-04", State: "ACTIVE"},
	}
	got := sameDayWindow(window, 15)
	if got[0].MeasuredUntil != "2026-06-16" || got[1].MeasuredUntil != "2026-07-16" || got[2].MeasuredUntil != "2026-08-04" {
		t.Fatalf("cuts=%v %v %v", got[0].MeasuredUntil, got[1].MeasuredUntil, got[2].MeasuredUntil)
	}
	if short := sameDayWindow(window, 25); short[1].MeasuredUntil != end2 {
		t.Fatalf("a cycle shorter than the elapsed days is measured to its own end: %s", short[1].MeasuredUntil)
	}
	if window[0].MeasuredUntil != end1 {
		t.Fatal("the input window must not be mutated")
	}
}

func TestBuildHistoryAddsTotalsSameDayAndDeltasThatReconcile(t *testing.T) {
	full := []cycleMeasure{
		historyMeasure("2026-05-01", map[string]string{"a": "1000", "b": "500"}),
		historyMeasure("2026-06-01", map[string]string{"a": "900", "b": "700", "c": "50"}),
		historyMeasure("2026-07-01", map[string]string{"a": "300", "b": "100"}),
	}
	measures := append([]cycleMeasure{}, full...)
	measures = append(measures,
		historyMeasure("2026-05-01", map[string]string{"a": "400", "b": "100"}),
		historyMeasure("2026-06-01", map[string]string{"a": "350", "b": "300"}),
	)
	_, matrix := buildHistory(measures, []int{0, 1, 2}, []int{3, 4, 2}, "2026-07-01")
	if matrix.SelectedIndex == nil || *matrix.SelectedIndex != 2 || strings.Join(matrix.Totals, ",") != "1500,1650,400" {
		t.Fatalf("selected=%v totals=%v", matrix.SelectedIndex, matrix.Totals)
	}
	if matrix.TotalDeltaVsPrevious == nil || *matrix.TotalDeltaVsPrevious != "-1250" {
		t.Fatalf("full total delta=%v", matrix.TotalDeltaVsPrevious)
	}
	sd := matrix.SameDay
	if sd == nil || strings.Join(sd.Totals, ",") != "500,650,400" || sd.TotalDeltaVsPrevious == nil || *sd.TotalDeltaVsPrevious != "-250" {
		t.Fatalf("same-day=%+v", sd)
	}
	for name, m := range map[string]categoryMatrix{"full": matrix.categoryMatrix, "sameDay": sd.categoryMatrix} {
		for i := range m.Totals {
			listed := m.Other.Amounts[i]
			for _, row := range m.Rows {
				listed = financialmath.Add(listed, row.Amounts[i])
			}
			if listed != m.Totals[i] {
				t.Fatalf("%s column %d: rows+other=%s total=%s", name, i, listed, m.Totals[i])
			}
		}
	}
	for _, row := range sd.Rows {
		if row.ID == "a" && (row.DeltaVsPrevious == nil || *row.DeltaVsPrevious != "-50") {
			t.Fatalf("same-day delta for a: %+v", row)
		}
	}
	for _, row := range matrix.Rows {
		if row.ID == "a" && (row.DeltaVsPrevious == nil || *row.DeltaVsPrevious != "-600") {
			t.Fatalf("full delta for a: %+v", row)
		}
	}
	if _, closed := buildHistory(measures, []int{0, 1, 2}, nil, "2026-07-01"); closed.SameDay != nil {
		t.Fatal("same-day is only present while the selected cycle is active")
	}
	_, oldest := buildHistory(measures, []int{0, 1, 2}, nil, "2026-05-01")
	if oldest.TotalDeltaVsPrevious != nil || oldest.Rows[0].DeltaVsPrevious != nil {
		t.Fatal("the oldest column has no previous cycle")
	}
}

func rankMeasure(expense string, merchants map[string]reviewValue) cycleMeasure {
	m := newCycleMeasure(reviewPeriod{Start: "2026-06-01", MeasuredUntil: "2026-07-01", State: "CLOSED"})
	m.merchants, m.cash.Expense = merchants, expense
	return m
}

func TestMerchantRankingIsNumericAndReconcilesToNetExpense(t *testing.T) {
	merchants, previous := map[string]reviewValue{}, map[string]reviewValue{}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("m%02d", i)
		merchants[id+":x"] = reviewValue{ID: id, Name: "Merchant " + id, Amount: fmt.Sprint(1000 * (i + 1)), Count: i + 1}
	}
	merchants[":tanpa merchant"] = reviewValue{Name: "Tanpa merchant", Amount: "9", Count: 2}
	merchants["m99:refunded"] = reviewValue{ID: "m99", Name: "Refunded", Amount: "-50", Count: 1}
	merchants["m98:zero"] = reviewValue{ID: "m98", Name: "Zero", Amount: "0", Count: 2}
	previous["m11:x"] = reviewValue{ID: "m11", Name: "Merchant m11", Amount: "8000", Count: 3}
	previous["m00:x"] = reviewValue{ID: "m00", Name: "Merchant m00", Amount: "400", Count: 1}
	previous["m50:gone"] = reviewValue{ID: "m50", Name: "Gone", Amount: "123", Count: 1}
	sum, count := "0", 0
	for _, v := range merchants {
		sum = financialmath.Add(sum, v.Amount)
		count += v.Count
	}
	cur, prev := rankMeasure(sum, merchants), rankMeasure("20000", previous)
	r := buildMerchantRanking([]cycleMeasure{cur, prev}, "FULL_CYCLE")
	if len(r.Rows) != 10 || r.Rows[0].ID != "m11" || r.Rows[1].ID != "m10" || r.Rows[9].ID != "m02" {
		t.Fatalf("rows must be the ten largest, numerically (12000 before 2000): %+v", r.Rows)
	}
	if r.Rows[0].Kind != "MERCHANT" || *r.Rows[0].Previous != "8000" || *r.Rows[0].Delta != "4000" || *r.Rows[9].Previous != "0" {
		t.Fatalf("row previous/delta: %+v", r.Rows[0])
	}
	listed, listedPrev, listedCount := "0", "0", 0
	for _, row := range r.Rows {
		listed = financialmath.Add(listed, row.Amount)
		listedPrev = financialmath.Add(listedPrev, *row.Previous)
		listedCount += row.Count
	}
	if financialmath.Add(listed, r.Other.Amount) != sum || r.Total.Amount != sum || r.Total.Count != count || listedCount+r.Other.Count != count {
		t.Fatalf("rows+other must equal net expense: listed=%s other=%+v total=%+v sum=%s", listed, r.Other, r.Total, sum)
	}
	if financialmath.Add(listedPrev, *r.Other.Previous) != "20000" || *r.Total.Previous != "20000" || *r.Total.Delta != financialmath.Subtract(sum, "20000") {
		t.Fatalf("previous side must reconcile: %+v %+v", r.Other, r.Total)
	}
	if financialmath.Add(*r.Other.Delta, financialmath.Subtract(listed, listedPrev)) != *r.Total.Delta {
		t.Fatal("row deltas + other delta must equal the total delta")
	}
	if r.Other.MerchantCount != 5 {
		t.Fatalf("other merchant count=%d", r.Other.MerchantCount)
	}
	if merchantKind(merchants[":tanpa merchant"]) != "NONE" || merchantKind(reviewValue{Name: "Warung X"}) != "COUNTERPARTY" {
		t.Fatal("kind must tell the no-merchant row from a named, unresolved payee")
	}
	solo := buildMerchantRanking([]cycleMeasure{cur}, "FULL_CYCLE")
	if solo.Total.Previous != nil || solo.Total.Delta != nil || solo.Rows[0].Previous != nil || solo.Other.Delta != nil {
		t.Fatal("without a comparison cycle nothing is compared")
	}
}

func TestOrderedMembersPutsAutomaticFirstAndSharesSumToOne(t *testing.T) {
	members := orderedMembers(map[string]reviewValue{
		"u2": {ID: "u2", Name: "Budi", Amount: "1", Count: 1},
		"":   {ID: "", Name: "Bersama / otomatis / belum diatribusikan", Amount: "1", Count: 1},
		"u1": {ID: "u1", Name: "Ani", Amount: "1", Count: 1},
	})
	if members[0].Kind != "AUTOMATIC" || members[1].Name != "Ani" || members[2].Name != "Budi" || members[1].Kind != "MEMBER" {
		t.Fatalf("order=%+v", members)
	}
	total := 0
	for _, m := range members {
		var whole, frac int
		if _, err := fmt.Sscanf(strings.Replace(*m.Share, ".", " ", 1), "%d %d", &whole, &frac); err != nil {
			t.Fatal(err)
		}
		total += whole*10000 + frac
	}
	if total != 10000 || *members[0].Share != "0.3334" || *members[1].Share != "0.3333" {
		t.Fatalf("thirds must round to exactly 100%%: %+v", members)
	}
	if got := orderedMembers(map[string]reviewValue{"u": {ID: "u", Name: "A", Amount: "0"}}); got[0].Share != nil {
		t.Fatal("no positive spending means no shares")
	}
	if got := orderedMembers(map[string]reviewValue{"u": {ID: "u", Name: "A", Amount: "-5"}, "v": {ID: "v", Name: "B", Amount: "50"}}); got[0].Share != nil || got[1].Share != nil {
		t.Fatal("a net-refund member makes shares undefined, never negative")
	}
}
